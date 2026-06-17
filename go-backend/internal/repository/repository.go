package repository

import (
	"encoding/json"
	"fmt"
	"thz-service/internal/models"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(host string, port int, user, password, dbname string) (*Repository, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &Repository{db: db}, nil
}

func (r *Repository) Begin() *Repository {
	return &Repository{db: r.db.Begin()}
}

func (r *Repository) Commit() error {
	return r.db.Commit().Error
}

func (r *Repository) Rollback() error {
	return r.db.Rollback().Error
}

func (r *Repository) DB() *gorm.DB {
	return r.db
}

func (r *Repository) CreateAnalysis(analysis *models.Analysis) error {
	return r.db.Create(analysis).Error
}

func (r *Repository) UpdateAnalysisStatus(analysisID string, status models.AnalysisStatus, errMsg string) error {
	updates := map[string]interface{}{"status": status}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}
	if status == models.StatusCompleted || status == models.StatusFailed || status == models.StatusInvalid {
		now := time.Now()
		updates["completed_at"] = &now
	}
	if status == models.StatusInvalid {
		updates["is_valid"] = false
	}
	return r.db.Model(&models.Analysis{}).Where("id = ?", analysisID).Updates(updates).Error
}

func (r *Repository) UpdateMoistureResult(analysisID string, moisture float64) error {
	return r.db.Model(&models.Analysis{}).Where("id = ?", analysisID).
		Updates(map[string]interface{}{
			"moisture_content_percent": moisture,
			"status":                   models.StatusCompleted,
			"completed_at":             time.Now(),
		}).Error
}

func (r *Repository) CreateRawWaveform(wf *models.RawWaveform) error {
	return r.db.Create(wf).Error
}

func (r *Repository) CreateFrequencySpectrum(fs *models.FrequencySpectrum) error {
	return r.db.Create(fs).Error
}

func (r *Repository) CreateOpticalParam(op *models.OpticalParam) error {
	return r.db.Create(op).Error
}

func (r *Repository) GetAnalysis(analysisID string) (*models.Analysis, error) {
	var analysis models.Analysis
	err := r.db.Where("id = ?", analysisID).First(&analysis).Error
	if err != nil {
		return nil, err
	}
	return &analysis, nil
}

func (r *Repository) ListAnalyses(limit, offset int) ([]models.Analysis, int64, error) {
	var analyses []models.Analysis
	var total int64

	r.db.Model(&models.Analysis{}).Count(&total)
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&analyses).Error
	return analyses, total, err
}

func (r *Repository) GetRawWaveform(analysisID string) (*models.RawWaveform, error) {
	var wf models.RawWaveform
	err := r.db.Where("analysis_id = ?", analysisID).First(&wf).Error
	if err != nil {
		return nil, err
	}
	return &wf, nil
}

func (r *Repository) GetFrequencySpectrum(analysisID string) (*models.FrequencySpectrum, error) {
	var fs models.FrequencySpectrum
	err := r.db.Where("analysis_id = ?", analysisID).First(&fs).Error
	if err != nil {
		return nil, err
	}
	return &fs, nil
}

func (r *Repository) GetOpticalParam(analysisID string) (*models.OpticalParam, error) {
	var op models.OpticalParam
	err := r.db.Where("analysis_id = ?", analysisID).First(&op).Error
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (r *Repository) GetAnalysisDetail(analysisID string) (*models.AnalysisDetail, error) {
	analysis, err := r.GetAnalysis(analysisID)
	if err != nil {
		return nil, err
	}

	detail := &models.AnalysisDetail{Analysis: analysis}

	if wf, err := r.GetRawWaveform(analysisID); err == nil {
		detail.RawWaveform = wf
	}
	if fs, err := r.GetFrequencySpectrum(analysisID); err == nil {
		detail.FrequencySpectrum = fs
	}
	if op, err := r.GetOpticalParam(analysisID); err == nil {
		detail.OpticalParam = op
	}

	return detail, nil
}

func (r *Repository) SaveFFTResult(analysisID string, fft *models.FFTResult) error {
	freqJSON, _ := json.Marshal(fft.Frequencies)
	ampJSON, _ := json.Marshal(fft.SampleAmplitude)

	fs := &models.FrequencySpectrum{
		AnalysisID:      analysisID,
		Frequencies:     datatypes.JSON(freqJSON),
		SampleAmplitude: datatypes.JSON(ampJSON),
		SamplePhase:     models.NumericArray(fft.SamplePhase),
	}

	if fft.ReferenceAmplitude != nil {
		refAmpJSON, _ := json.Marshal(fft.ReferenceAmplitude)
		fs.ReferenceAmplitude = datatypes.JSON(refAmpJSON)
	}
	if fft.ReferencePhase != nil {
		fs.ReferencePhase = models.NumericArray(fft.ReferencePhase)
	}
	if fft.SampleRealPart != nil {
		fs.SampleRealPart = models.NumericArray(fft.SampleRealPart)
	}
	if fft.SampleImagPart != nil {
		fs.SampleImagPart = models.NumericArray(fft.SampleImagPart)
	}
	if fft.ReferenceRealPart != nil {
		fs.ReferenceRealPart = models.NumericArray(fft.ReferenceRealPart)
	}
	if fft.ReferenceImagPart != nil {
		fs.ReferenceImagPart = models.NumericArray(fft.ReferenceImagPart)
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(fs).Error; err != nil {
			return err
		}
		return tx.Model(&models.Analysis{}).Where("id = ?", analysisID).
			Update("status", models.StatusFFTDone).Error
	})
}

func (r *Repository) SaveParamsResult(analysisID string, params *models.ParamsResult) error {
	freqJSON, _ := json.Marshal(params.Frequencies)
	alphaJSON, _ := json.Marshal(params.AbsorptionCoeff)
	nJSON, _ := json.Marshal(params.RefractiveIndex)

	op := &models.OpticalParam{
		AnalysisID:      analysisID,
		Frequencies:     datatypes.JSON(freqJSON),
		AbsorptionCoeff: datatypes.JSON(alphaJSON),
		RefractiveIndex: datatypes.JSON(nJSON),
	}

	if params.ExtinctionCoeff != nil {
		kJSON, _ := json.Marshal(params.ExtinctionCoeff)
		op.ExtinctionCoeff = datatypes.JSON(kJSON)
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		return tx.Model(&models.Analysis{}).Where("id = ?", analysisID).
			Update("status", models.StatusParamsDone).Error
	})
}

func (r *Repository) SaveCompleteResult(result *models.WorkerResultMessage) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"status":      models.StatusCompleted,
			"is_valid":    true,
			"completed_at": time.Now(),
		}

		if result.Moisture != nil {
			updates["moisture_content_percent"] = *result.Moisture
		}

		if result.AnomalyDetection != nil {
			updates["anomaly_score"] = result.AnomalyDetection.AnomalyScore
			updates["anomaly_confidence"] = result.AnomalyDetection.Confidence
			updates["anomaly_severity"] = result.AnomalyDetection.Severity

			if len(result.AnomalyDetection.Reasons) > 0 {
				reasonsJSON, _ := json.Marshal(result.AnomalyDetection.Reasons)
				updates["anomaly_reasons"] = datatypes.JSON(reasonsJSON)
			}

			typesJSON, _ := json.Marshal(result.AnomalyDetection.AnomalyType)
			updates["anomaly_types"] = datatypes.JSON(typesJSON)
		}

		if result.Performance != nil {
			updates["total_speedup_ratio"] = result.Performance.TotalSpeedup
			updates["prediction_time_ms"] = result.Performance.PredictionTimeMs
		}

		if result.FFT != nil && result.FFT.BandInfo != nil {
			updates["band_start_freq_thz"] = result.FFT.BandInfo.StartFreqHz / 1e12
			updates["band_end_freq_thz"] = result.FFT.BandInfo.EndFreqHz / 1e12
		}

		return tx.Model(&models.Analysis{}).Where("id = ?", result.AnalysisID).Updates(updates).Error
	})
}

func (r *Repository) SaveInvalidResult(result *models.WorkerResultMessage) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"status":       models.StatusInvalid,
			"is_valid":     false,
			"completed_at": time.Now(),
		}

		if result.AnomalyDetection != nil {
			updates["anomaly_score"] = result.AnomalyDetection.AnomalyScore
			updates["anomaly_confidence"] = result.AnomalyDetection.Confidence
			updates["anomaly_severity"] = result.AnomalyDetection.Severity

			if len(result.AnomalyDetection.Reasons) > 0 {
				reasonsJSON, _ := json.Marshal(result.AnomalyDetection.Reasons)
				updates["anomaly_reasons"] = datatypes.JSON(reasonsJSON)
			}

			typesJSON, _ := json.Marshal(result.AnomalyDetection.AnomalyType)
			updates["anomaly_types"] = datatypes.JSON(typesJSON)
		}

		if result.Error != "" {
			updates["error_message"] = result.Error
		}

		if result.FFT != nil && result.FFT.BandInfo != nil {
			updates["band_start_freq_thz"] = result.FFT.BandInfo.StartFreqHz / 1e12
			updates["band_end_freq_thz"] = result.FFT.BandInfo.EndFreqHz / 1e12
		}

		if err := tx.Model(&models.Analysis{}).Where("id = ?", result.AnalysisID).Updates(updates).Error; err != nil {
			return err
		}

		if result.FFT != nil {
			if err := r.upsertFFTResult(tx, result.AnalysisID, result.FFT); err != nil {
				return err
			}
		}

		if result.Params != nil {
			if err := r.upsertParamsResult(tx, result.AnalysisID, result.Params); err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *Repository) upsertFFTResult(tx *gorm.DB, analysisID string, fft *models.FFTResult) error {
	var existing models.FrequencySpectrum
	err := tx.Where("analysis_id = ?", analysisID).First(&existing).Error
	if err == nil {
		return nil
	}

	freqJSON, _ := json.Marshal(fft.Frequencies)
	ampJSON, _ := json.Marshal(fft.SampleAmplitude)

	fs := &models.FrequencySpectrum{
		AnalysisID:      analysisID,
		Frequencies:     datatypes.JSON(freqJSON),
		SampleAmplitude: datatypes.JSON(ampJSON),
		SamplePhase:     models.NumericArray(fft.SamplePhase),
	}

	if fft.ReferenceAmplitude != nil {
		refAmpJSON, _ := json.Marshal(fft.ReferenceAmplitude)
		fs.ReferenceAmplitude = datatypes.JSON(refAmpJSON)
	}
	if fft.ReferencePhase != nil {
		fs.ReferencePhase = models.NumericArray(fft.ReferencePhase)
	}
	if fft.SampleRealPart != nil {
		fs.SampleRealPart = models.NumericArray(fft.SampleRealPart)
	}
	if fft.SampleImagPart != nil {
		fs.SampleImagPart = models.NumericArray(fft.SampleImagPart)
	}
	if fft.ReferenceRealPart != nil {
		fs.ReferenceRealPart = models.NumericArray(fft.ReferenceRealPart)
	}
	if fft.ReferenceImagPart != nil {
		fs.ReferenceImagPart = models.NumericArray(fft.ReferenceImagPart)
	}

	return tx.Create(fs).Error
}

func (r *Repository) upsertParamsResult(tx *gorm.DB, analysisID string, params *models.ParamsResult) error {
	var existing models.OpticalParam
	err := tx.Where("analysis_id = ?", analysisID).First(&existing).Error
	if err == nil {
		return nil
	}

	freqJSON, _ := json.Marshal(params.Frequencies)
	alphaJSON, _ := json.Marshal(params.AbsorptionCoeff)
	nJSON, _ := json.Marshal(params.RefractiveIndex)

	op := &models.OpticalParam{
		AnalysisID:      analysisID,
		Frequencies:     datatypes.JSON(freqJSON),
		AbsorptionCoeff: datatypes.JSON(alphaJSON),
		RefractiveIndex: datatypes.JSON(nJSON),
	}

	if params.ExtinctionCoeff != nil {
		kJSON, _ := json.Marshal(params.ExtinctionCoeff)
		op.ExtinctionCoeff = datatypes.JSON(kJSON)
	}

	return tx.Create(op).Error
}

func (r *Repository) CreateDifferentialComparison(dc *models.DifferentialComparison) error {
	return r.db.Create(dc).Error
}

func (r *Repository) GetDifferentialComparison(id string) (*models.DifferentialComparison, error) {
	var dc models.DifferentialComparison
	err := r.db.Where("id = ?", id).First(&dc).Error
	if err != nil {
		return nil, err
	}
	return &dc, nil
}

func (r *Repository) ListDifferentialComparisons(limit, offset int) ([]models.DifferentialComparison, int64, error) {
	var comparisons []models.DifferentialComparison
	var total int64

	r.db.Model(&models.DifferentialComparison{}).Count(&total)
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&comparisons).Error
	return comparisons, total, err
}

func (r *Repository) UpdateDifferentialResult(dcID string, result *models.DifferentialResult) error {
	diffSpecJSON, _ := json.Marshal(result.DifferenceSpectrum)

	updates := map[string]interface{}{
		"status":               models.StatusCompleted,
		"migration_rate_per_hour": result.MigrationRate,
		"delta_moisture":       result.DeltaMoisture,
		"moisture_t1":          result.MoistureT1,
		"moisture_t2":          result.MoistureT2,
		"drying_efficiency":    result.DryingEfficiency,
		"completed_at":         time.Now(),
	}

	if result.HalfLifeHours != nil {
		updates["half_life_hours"] = *result.HalfLifeHours
	}
	if result.DifferenceSpectrum != nil {
		updates["difference_spectrum"] = datatypes.JSON(diffSpecJSON)
	}

	return r.db.Model(&models.DifferentialComparison{}).Where("id = ?", dcID).Updates(updates).Error
}

func (r *Repository) UpdateDifferentialStatus(dcID string, status models.AnalysisStatus, errMsg string) error {
	updates := map[string]interface{}{"status": status}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}
	if status == models.StatusCompleted || status == models.StatusFailed || status == models.StatusInvalid {
		updates["completed_at"] = time.Now()
	}
	return r.db.Model(&models.DifferentialComparison{}).Where("id = ?", dcID).Updates(updates).Error
}

func (r *Repository) CreateCacheRecord(cr *models.CacheRecord) error {
	return r.db.Create(cr).Error
}

func (r *Repository) GetCacheRecordByMD5(md5 string) (*models.CacheRecord, error) {
	var cr models.CacheRecord
	err := r.db.Where("md5 = ?", md5).First(&cr).Error
	if err != nil {
		return nil, err
	}
	return &cr, nil
}

func (r *Repository) IncrementCacheHit(md5 string) error {
	return r.db.Model(&models.CacheRecord{}).Where("md5 = ?", md5).
		Updates(map[string]interface{}{
			"hit_count":      gorm.Expr("hit_count + 1"),
			"last_accessed_at": time.Now(),
		}).Error
}

func (r *Repository) GetDifferentialComparisonDetail(id string) (*models.DifferentialComparisonDetail, error) {
	dc, err := r.GetDifferentialComparison(id)
	if err != nil {
		return nil, err
	}

	detail := &models.DifferentialComparisonDetail{Comparison: dc}

	if a1, err := r.GetAnalysisDetail(dc.AnalysisID_T1); err == nil {
		detail.AnalysisT1 = a1
	}
	if a2, err := r.GetAnalysisDetail(dc.AnalysisID_T2); err == nil {
		detail.AnalysisT2 = a2
	}

	if dc.MigrationRate != nil {
		detail.Result = &models.DifferentialResult{
			MigrationRate:    *dc.MigrationRate,
			DeltaMoisture:    *dc.DeltaMoisture,
			MoistureT1:       *dc.MoistureT1,
			MoistureT2:       *dc.MoistureT2,
			DryingEfficiency: *dc.DryingEfficiency,
			IsDrying:         *dc.MoistureT2 < *dc.MoistureT1,
		}
		if dc.HalfLifeHours != nil {
			detail.Result.HalfLifeHours = dc.HalfLifeHours
		}
	}

	return detail, nil
}
