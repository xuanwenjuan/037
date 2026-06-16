package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"thz-service/internal/models"
	"thz-service/internal/rabbitmq"
	"thz-service/internal/repository"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

type Handler struct {
	repo     *repository.Repository
	producer *rabbitmq.Producer
}

func NewHandler(repo *repository.Repository, producer *rabbitmq.Producer) *Handler {
	return &Handler{
		repo:     repo,
		producer: producer,
	}
}

func (h *Handler) UploadWaveform(c *gin.Context) {
	sampleName := c.PostForm("sample_name")
	materialType := c.PostForm("material_type")
	thicknessStr := c.PostForm("sample_thickness_mm")

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

	task := &models.TaskMessage{
		AnalysisID:        analysis.ID,
		SampleName:        sampleName,
		MaterialType:      materialType,
		SampleThicknessMM: thickness,
		Waveform:          *waveform,
		Timestamp:         time.Now(),
	}

	if err := h.producer.PublishTask(task); err != nil {
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
