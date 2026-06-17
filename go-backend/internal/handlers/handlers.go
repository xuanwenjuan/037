package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"thz-service/internal/cache"
	"thz-service/internal/metrics"
	"thz-service/internal/models"
	"thz-service/internal/rabbitmq"
	"thz-service/internal/repository"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

type Handler struct {
	repo        *repository.Repository
	producer    *rabbitmq.Producer
	cache       *cache.CacheService
	metrics     *metrics.MetricsCollector
	enableCache bool
}

func NewHandler(repo *repository.Repository, producer *rabbitmq.Producer, cache *cache.CacheService, metrics *metrics.MetricsCollector, enableCache bool) *Handler {
	return &Handler{
		repo:        repo,
		producer:    producer,
		cache:       cache,
		metrics:     metrics,
		enableCache: enableCache,
	}
}

func (h *Handler) UploadWaveform(c *gin.Context) {
	sampleName := c.PostForm("sample_name")
	materialType := c.PostForm("material_type")
	thicknessStr := c.PostForm("sample_thickness_mm")
	skipCache, _ := strconv.ParseBool(c.DefaultPostForm("skip_cache", "false"))

	if sampleName == "" || thicknessStr == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "sample_name and sample_thickness_mm are required",
		})
		return
	}

	thickness, err := strconv.ParseFloat(thicknessStr, 64)
	if err != nil || thickness <= 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "invalid sample_thickness_mm, must be positive number",
		})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("failed to get file: %v", err),
		})
		return
	}
	defer file.Close()

	filename := header.Filename
	var waveform *models.WaveformData

	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		waveform, err = parseCSV(file)
	} else if strings.HasSuffix(strings.ToLower(filename), ".json") {
		waveform, err = parseJSON(file)
	} else {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "unsupported file format, must be .csv or .json",
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("failed to parse file: %v", err),
		})
		return
	}

	if err := validateWaveform(waveform); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: err.Error(),
		})
		return
	}

	if h.enableCache && !skipCache && h.cache != nil {
		md5 := cache.ComputeWaveformMD5(waveform.Time, waveform.SampleField)
		if cached, err := h.cache.Get(c.Request.Context(), md5); err == nil {
			if h.metrics != nil {
				h.metrics.CacheHits.Inc()
			}
			if err := h.repo.IncrementCacheHit(md5); err != nil {
				logWarn("failed to increment cache hit count: %v", err)
			}
			c.JSON(http.StatusOK, gin.H{
				"analysis_id":    cached.AnalysisID,
				"status":         "cached",
				"is_cached":      true,
				"md5":            md5,
				"hit_count":      cached.HitCount,
				"cached_at":      cached.CachedAt,
				"cached_result":  cached.Data,
			})
			return
		} else if h.metrics != nil {
			h.metrics.CacheMisses.Inc()
		}
	}

	analysis := &models.Analysis{
		SampleName:        sampleName,
		MaterialType:      materialType,
		SampleThicknessMM: thickness,
		Status:            models.StatusPending,
	}

	if err := h.repo.CreateAnalysis(analysis); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to create analysis: %v", err),
		})
		return
	}

	if h.enableCache && !skipCache && h.cache != nil {
		md5 := cache.ComputeWaveformMD5(waveform.Time, waveform.SampleField)
		cacheRecord := &models.CacheRecord{
			ID:              md5,
			MD5:             md5,
			AnalysisID:      analysis.ID,
			TimePointsHash:  fmt.Sprintf("%x", hashFloatSlice(waveform.Time)),
			SampleFieldHash: fmt.Sprintf("%x", hashFloatSlice(waveform.SampleField)),
			HitCount:        0,
			CreatedAt:       time.Now(),
			LastAccessedAt:  time.Now(),
		}
		if err := h.repo.CreateCacheRecord(cacheRecord); err != nil {
			logWarn("failed to create cache record: %v", err)
		}
	}

	timeJSON, _ := json.Marshal(waveform.Time)
	sampleJSON, _ := json.Marshal(waveform.SampleField)
	rawWaveform := &models.RawWaveform{
		AnalysisID:  analysis.ID,
		TimePoints:  datatypes.JSON(timeJSON),
		SampleField: datatypes.JSON(sampleJSON),
	}
	if waveform.ReferenceField != nil {
		refJSON, _ := json.Marshal(waveform.ReferenceField)
		rawWaveform.ReferenceField = datatypes.JSON(refJSON)
	}

	if err := h.repo.CreateRawWaveform(rawWaveform); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to save waveform: %v", err),
		})
		return
	}

	if h.metrics != nil {
		h.metrics.TasksTotal.Inc()
		h.metrics.TaskQueueDepth.Inc()
	}

	task := &models.TaskMessage{
		AnalysisID:        analysis.ID,
		SampleName:        sampleName,
		MaterialType:      materialType,
		SampleThicknessMM: thickness,
		Waveform:          *waveform,
		Timestamp:         time.Now(),
	}

	if err := h.producer.PublishTask(task); err != nil {
		if h.metrics != nil {
			h.metrics.TasksFailed.Inc()
			h.metrics.TaskQueueDepth.Dec()
		}
		h.repo.UpdateAnalysisStatus(analysis.ID, models.StatusFailed, err.Error())
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to queue task: %v", err),
		})
		return
	}

	if err := h.repo.UpdateAnalysisStatus(analysis.ID, models.StatusQueued, ""); err != nil {
		logWarn("failed to update status to queued: %v", err)
	}

	c.JSON(http.StatusAccepted, models.UploadResponse{
		AnalysisID: analysis.ID,
		Status:     string(models.StatusQueued),
		Message:    "Waveform uploaded and queued for processing",
	})
}

func (h *Handler) GetAnalysis(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "analysis id is required"})
		return
	}

	detail, err := h.repo.GetAnalysisDetail(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "analysis not found"})
		return
	}

	c.JSON(http.StatusOK, detail)
}

func (h *Handler) ListAnalyses(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if limit < 1 || limit > 100 {
		limit = 20
	}

	analyses, total, err := h.repo.ListAnalyses(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to list analyses: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  analyses,
		"total": total,
		"limit": limit,
		"offset": offset,
	})
}

func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

func (h *Handler) BatchUpload(c *gin.Context) {
	sampleNamePrefix := c.PostForm("sample_name_prefix")
	materialType := c.PostForm("material_type")
	thicknessStr := c.PostForm("sample_thickness_mm")
	skipCache, _ := strconv.ParseBool(c.DefaultPostForm("skip_cache", "false"))

	if sampleNamePrefix == "" || thicknessStr == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "sample_name_prefix and sample_thickness_mm are required",
		})
		return
	}

	thickness, err := strconv.ParseFloat(thicknessStr, 64)
	if err != nil || thickness <= 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "invalid sample_thickness_mm, must be positive number",
		})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("failed to get multipart form: %v", err),
		})
		return
	}
	defer form.RemoveAll()

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "no files uploaded",
		})
		return
	}

	result := &models.BatchUploadResult{
		TotalCount: len(files),
		Results:    make([]models.BatchUploadItemResult, 0, len(files)),
		Status:     "partial",
	}

	for i, fileHeader := range files {
		itemResult := h.processSingleFile(
			c.Request.Context(),
			fileHeader,
			fmt.Sprintf("%s_%d", sampleNamePrefix, i+1),
			materialType,
			thickness,
			skipCache,
		)
		itemResult.Index = i
		itemResult.FileName = fileHeader.Filename

		result.Results = append(result.Results, itemResult)

		if itemResult.Status == "queued" || itemResult.Status == "cached" {
			result.SuccessCount++
			result.AnalysisIDs = append(result.AnalysisIDs, itemResult.AnalysisID)
			if itemResult.IsDuplicate {
				result.DuplicateCount++
			}
		} else {
			result.FailedCount++
		}
	}

	if result.FailedCount == 0 {
		result.Status = "all_success"
	} else if result.SuccessCount == 0 {
		result.Status = "all_failed"
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) processSingleFile(ctx context.Context, fileHeader *multipart.FileHeader, sampleName, materialType string, thickness float64, skipCache bool) models.BatchUploadItemResult {
	file, err := fileHeader.Open()
	if err != nil {
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: fmt.Sprintf("failed to open file: %v", err),
		}
	}
	defer file.Close()

	filename := fileHeader.Filename
	var waveform *models.WaveformData

	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		waveform, err = parseCSV(file)
	} else if strings.HasSuffix(strings.ToLower(filename), ".json") {
		waveform, err = parseJSON(file)
	} else {
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: "unsupported file format, must be .csv or .json",
		}
	}

	if err != nil {
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: fmt.Sprintf("failed to parse file: %v", err),
		}
	}

	if err := validateWaveform(waveform); err != nil {
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: err.Error(),
		}
	}

	md5 := cache.ComputeWaveformMD5(waveform.Time, waveform.SampleField)

	if h.enableCache && !skipCache && h.cache != nil {
		if cached, err := h.cache.Get(ctx, md5); err == nil {
			if h.metrics != nil {
				h.metrics.CacheHits.Inc()
			}
			if err := h.repo.IncrementCacheHit(md5); err != nil {
				logWarn("failed to increment cache hit: %v", err)
			}
			return models.BatchUploadItemResult{
				AnalysisID:  cached.AnalysisID,
				Status:      "cached",
				Message:     "cached result returned",
				IsDuplicate: true,
				MD5:         md5,
			}
		} else if h.metrics != nil {
			h.metrics.CacheMisses.Inc()
		}
	}

	analysis := &models.Analysis{
		SampleName:        sampleName,
		MaterialType:      materialType,
		SampleThicknessMM: thickness,
		Status:            models.StatusPending,
	}

	if err := h.repo.CreateAnalysis(analysis); err != nil {
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: fmt.Sprintf("failed to create analysis: %v", err),
		}
	}

	if h.enableCache && !skipCache && h.cache != nil {
		cacheRecord := &models.CacheRecord{
			ID:              md5,
			MD5:             md5,
			AnalysisID:      analysis.ID,
			TimePointsHash:  fmt.Sprintf("%x", hashFloatSlice(waveform.Time)),
			SampleFieldHash: fmt.Sprintf("%x", hashFloatSlice(waveform.SampleField)),
			HitCount:        0,
			CreatedAt:       time.Now(),
			LastAccessedAt:  time.Now(),
		}
		if err := h.repo.CreateCacheRecord(cacheRecord); err != nil {
			logWarn("failed to create cache record: %v", err)
		}
	}

	timeJSON, _ := json.Marshal(waveform.Time)
	sampleJSON, _ := json.Marshal(waveform.SampleField)
	rawWaveform := &models.RawWaveform{
		AnalysisID:  analysis.ID,
		TimePoints:  datatypes.JSON(timeJSON),
		SampleField: datatypes.JSON(sampleJSON),
	}
	if waveform.ReferenceField != nil {
		refJSON, _ := json.Marshal(waveform.ReferenceField)
		rawWaveform.ReferenceField = datatypes.JSON(refJSON)
	}

	if err := h.repo.CreateRawWaveform(rawWaveform); err != nil {
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: fmt.Sprintf("failed to save waveform: %v", err),
		}
	}

	if h.metrics != nil {
		h.metrics.TasksTotal.Inc()
		h.metrics.TaskQueueDepth.Inc()
	}

	task := &models.TaskMessage{
		AnalysisID:        analysis.ID,
		SampleName:        sampleName,
		MaterialType:      materialType,
		SampleThicknessMM: thickness,
		Waveform:          *waveform,
		Timestamp:         time.Now(),
	}

	if err := h.producer.PublishTask(task); err != nil {
		if h.metrics != nil {
			h.metrics.TasksFailed.Inc()
			h.metrics.TaskQueueDepth.Dec()
		}
		h.repo.UpdateAnalysisStatus(analysis.ID, models.StatusFailed, err.Error())
		return models.BatchUploadItemResult{
			Status:  "failed",
			Message: fmt.Sprintf("failed to queue task: %v", err),
		}
	}

	if err := h.repo.UpdateAnalysisStatus(analysis.ID, models.StatusQueued, ""); err != nil {
		logWarn("failed to update status: %v", err)
	}

	return models.BatchUploadItemResult{
		AnalysisID:  analysis.ID,
		Status:      "queued",
		Message:     "queued for processing",
		IsDuplicate: false,
		MD5:         md5,
	}
}

func (h *Handler) DifferentialCompare(c *gin.Context) {
	var req struct {
		MaterialType      string  `json:"material_type" binding:"required"`
		SampleThicknessMM float64 `json:"sample_thickness_mm" binding:"required,gt=0"`
		TimeIntervalHours float64 `json:"time_interval_hours" binding:"required,gt=0"`
		WaveformT1        struct {
			Time          []float64 `json:"time" binding:"required,min=32"`
			SampleField   []float64 `json:"sample_field" binding:"required,min=32"`
			ReferenceField []float64 `json:"reference_field,omitempty"`
		} `json:"waveform_t1" binding:"required"`
		WaveformT2 struct {
			Time          []float64 `json:"time" binding:"required,min=32"`
			SampleField   []float64 `json:"sample_field" binding:"required,min=32"`
			ReferenceField []float64 `json:"reference_field,omitempty"`
		} `json:"waveform_t2" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	waveformT1 := &models.WaveformData{
		Time:           req.WaveformT1.Time,
		SampleField:    req.WaveformT1.SampleField,
		ReferenceField: req.WaveformT1.ReferenceField,
	}
	waveformT2 := &models.WaveformData{
		Time:           req.WaveformT2.Time,
		SampleField:    req.WaveformT2.SampleField,
		ReferenceField: req.WaveformT2.ReferenceField,
	}

	if err := validateWaveform(waveformT1); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("invalid waveform_t1: %v", err),
		})
		return
	}
	if err := validateWaveform(waveformT2); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: fmt.Sprintf("invalid waveform_t2: %v", err),
		})
		return
	}

	analysisT1 := &models.Analysis{
		SampleName:        fmt.Sprintf("%s_t1_%.2fh", req.MaterialType, 0),
		MaterialType:      req.MaterialType,
		SampleThicknessMM: req.SampleThicknessMM,
		Status:            models.StatusPending,
	}
	analysisT2 := &models.Analysis{
		SampleName:        fmt.Sprintf("%s_t2_%.2fh", req.MaterialType, req.TimeIntervalHours),
		MaterialType:      req.MaterialType,
		SampleThicknessMM: req.SampleThicknessMM,
		Status:            models.StatusPending,
	}

	tx := h.repo.Begin()
	if err := tx.CreateAnalysis(analysisT1); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to create analysis t1: %v", err),
		})
		return
	}
	if err := tx.CreateAnalysis(analysisT2); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to create analysis t2: %v", err),
		})
		return
	}

	diffComp := &models.DifferentialComparison{
		MaterialType:      req.MaterialType,
		SampleThicknessMM: req.SampleThicknessMM,
		AnalysisID_T1:     analysisT1.ID,
		AnalysisID_T2:     analysisT2.ID,
		TimeIntervalHours: req.TimeIntervalHours,
		Status:            models.StatusPending,
	}
	if err := tx.CreateDifferentialComparison(diffComp); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to create differential comparison: %v", err),
		})
		return
	}
	tx.Commit()

	timeJSON1, _ := json.Marshal(waveformT1.Time)
	sampleJSON1, _ := json.Marshal(waveformT1.SampleField)
	rawT1 := &models.RawWaveform{
		AnalysisID:  analysisT1.ID,
		TimePoints:  datatypes.JSON(timeJSON1),
		SampleField: datatypes.JSON(sampleJSON1),
	}
	if waveformT1.ReferenceField != nil {
		refJSON1, _ := json.Marshal(waveformT1.ReferenceField)
		rawT1.ReferenceField = datatypes.JSON(refJSON1)
	}
	h.repo.CreateRawWaveform(rawT1)

	timeJSON2, _ := json.Marshal(waveformT2.Time)
	sampleJSON2, _ := json.Marshal(waveformT2.SampleField)
	rawT2 := &models.RawWaveform{
		AnalysisID:  analysisT2.ID,
		TimePoints:  datatypes.JSON(timeJSON2),
		SampleField: datatypes.JSON(sampleJSON2),
	}
	if waveformT2.ReferenceField != nil {
		refJSON2, _ := json.Marshal(waveformT2.ReferenceField)
		rawT2.ReferenceField = datatypes.JSON(refJSON2)
	}
	h.repo.CreateRawWaveform(rawT2)

	diffTask := &models.DifferentialTask{
		ID:                diffComp.ID,
		MaterialType:      req.MaterialType,
		SampleThicknessMM: req.SampleThicknessMM,
		TimeIntervalHours: req.TimeIntervalHours,
		WaveformT1:        *waveformT1,
		WaveformT2:        *waveformT2,
		Timestamp:         time.Now(),
	}

	if err := h.producer.PublishDifferentialTask(diffTask); err != nil {
		h.repo.UpdateDifferentialStatus(diffComp.ID, models.StatusFailed, err.Error())
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to queue differential task: %v", err),
		})
		return
	}

	h.repo.UpdateDifferentialStatus(diffComp.ID, models.StatusQueued, "")

	c.JSON(http.StatusAccepted, gin.H{
		"comparison_id": diffComp.ID,
		"analysis_id_t1": analysisT1.ID,
		"analysis_id_t2": analysisT2.ID,
		"status":         string(models.StatusQueued),
		"message":        "Differential comparison task queued for processing",
	})
}

func (h *Handler) GetDifferentialComparison(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "comparison id is required"})
		return
	}

	detail, err := h.repo.GetDifferentialComparisonDetail(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "comparison not found"})
		return
	}

	c.JSON(http.StatusOK, detail)
}

func (h *Handler) ListDifferentialComparisons(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if limit < 1 || limit > 100 {
		limit = 20
	}

	comparisons, total, err := h.repo.ListDifferentialComparisons(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to list comparisons: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  comparisons,
		"total": total,
		"limit": limit,
		"offset": offset,
	})
}

func (h *Handler) GetCacheStats(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{Error: "cache service is disabled"})
		return
	}

	stats, err := h.cache.GetStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: fmt.Sprintf("failed to get cache stats: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (h *Handler) GetMetricsSummary(c *gin.Context) {
	if h.metrics == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{Error: "metrics service is disabled"})
		return
	}

	resp := h.metrics.GetMetricsResponse()
	c.JSON(http.StatusOK, resp)
}

func parseCSV(r io.Reader) (*models.WaveformData, error) {
	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV must have at least header and one data row")
	}

	headers := records[0]
	colCount := len(headers)
	if colCount < 2 {
		return nil, fmt.Errorf("CSV must have at least time and sample_field columns")
	}

	timeIdx := -1
	sampleIdx := -1
	refIdx := -1

	for i, h := range headers {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "time" || h == "t" || h == "time_ps" {
			timeIdx = i
		} else if h == "sample" || h == "sample_field" || h == "e_sample" {
			sampleIdx = i
		} else if h == "reference" || h == "reference_field" || h == "e_ref" || h == "ref" {
			refIdx = i
		}
	}

	if timeIdx == -1 {
		timeIdx = 0
	}
	if sampleIdx == -1 {
		sampleIdx = 1
	}

	waveform := &models.WaveformData{}
	dataRows := records[1:]

	for _, row := range dataRows {
		if len(row) <= timeIdx || len(row) <= sampleIdx {
			continue
		}

		t, err := strconv.ParseFloat(strings.TrimSpace(row[timeIdx]), 64)
		if err != nil {
			continue
		}
		s, err := strconv.ParseFloat(strings.TrimSpace(row[sampleIdx]), 64)
		if err != nil {
			continue
		}

		waveform.Time = append(waveform.Time, t)
		waveform.SampleField = append(waveform.SampleField, s)

		if refIdx != -1 && len(row) > refIdx {
			r, err := strconv.ParseFloat(strings.TrimSpace(row[refIdx]), 64)
			if err == nil {
				waveform.ReferenceField = append(waveform.ReferenceField, r)
			}
		}
	}

	return waveform, nil
}

func parseJSON(r io.Reader) (*models.WaveformData, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON: %w", err)
	}

	var waveform models.WaveformData
	if err := json.Unmarshal(body, &waveform); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &waveform, nil
}

func validateWaveform(wf *models.WaveformData) error {
	if len(wf.Time) == 0 {
		return fmt.Errorf("time array is empty")
	}
	if len(wf.SampleField) == 0 {
		return fmt.Errorf("sample_field array is empty")
	}
	if len(wf.Time) != len(wf.SampleField) {
		return fmt.Errorf("time and sample_field arrays must have same length")
	}
	if wf.ReferenceField != nil && len(wf.Time) != len(wf.ReferenceField) {
		return fmt.Errorf("time and reference_field arrays must have same length")
	}
	if len(wf.Time) < 32 {
		return fmt.Errorf("waveform must have at least 32 data points for FFT")
	}
	return nil
}

func logWarn(format string, args ...interface{}) {
	fmt.Printf("WARN: "+format+"\n", args...)
}

func hashFloatSlice(data []float64) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, v := range data {
		b := make([]byte, 8)
		for i := 0; i < 8; i++ {
			b[i] = byte(uint64(v*1e12) >> (i * 8))
		}
		for _, bt := range b {
			h ^= uint64(bt)
			h *= 0x100000001b3
		}
	}
	return h
}
