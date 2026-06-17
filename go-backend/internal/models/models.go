package models

import (
	"time"

	"gorm.io/datatypes"
)

type AnalysisStatus string

const (
	StatusPending    AnalysisStatus = "pending"
	StatusQueued     AnalysisStatus = "queued"
	StatusProcessing AnalysisStatus = "processing"
	StatusFFTDone    AnalysisStatus = "fft_done"
	StatusParamsDone AnalysisStatus = "params_done"
	StatusCompleted  AnalysisStatus = "completed"
	StatusFailed     AnalysisStatus = "failed"
	StatusInvalid    AnalysisStatus = "invalid"
)

type Analysis struct {
	ID                      string         `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	SampleName              string         `gorm:"not null" json:"sample_name"`
	MaterialType            string         `json:"material_type"`
	SampleThicknessMM       float64        `gorm:"not null" json:"sample_thickness_mm"`
	Status                  AnalysisStatus `gorm:"not null;default:pending" json:"status"`
	ErrorMessage            string         `json:"error_message,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
	CompletedAt             *time.Time     `json:"completed_at,omitempty"`
	MoistureContentPercent  *float64       `json:"moisture_content_percent,omitempty"`
	IsValid                 bool           `gorm:"default:true" json:"is_valid"`
	AnomalyScore            *float64       `json:"anomaly_score,omitempty"`
	AnomalyConfidence       *float64       `json:"anomaly_confidence,omitempty"`
	AnomalySeverity         string         `json:"anomaly_severity,omitempty"`
	AnomalyReasons          datatypes.JSON `gorm:"type:jsonb" json:"anomaly_reasons,omitempty"`
	AnomalyTypes            datatypes.JSON `gorm:"type:jsonb" json:"anomaly_types,omitempty"`
	BandStartFreqTHz        *float64       `json:"band_start_freq_thz,omitempty"`
	BandEndFreqTHz          *float64       `json:"band_end_freq_thz,omitempty"`
	TotalSpeedupRatio       *float64       `json:"total_speedup_ratio,omitempty"`
	PredictionTimeMs        *float64       `json:"prediction_time_ms,omitempty"`
}

type RawWaveform struct {
	ID             string         `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	AnalysisID     string         `gorm:"type:uuid;not null" json:"analysis_id"`
	TimePoints     datatypes.JSON `gorm:"type:jsonb;not null" json:"time_points"`
	SampleField    datatypes.JSON `gorm:"type:jsonb;not null" json:"sample_field"`
	ReferenceField datatypes.JSON `gorm:"type:jsonb" json:"reference_field,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type FrequencySpectrum struct {
	ID                string         `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	AnalysisID        string         `gorm:"type:uuid;not null" json:"analysis_id"`
	Frequencies       datatypes.JSON `gorm:"type:jsonb;not null" json:"frequencies"`
	SampleAmplitude   datatypes.JSON `gorm:"type:jsonb;not null" json:"sample_amplitude"`
	SamplePhase       datatypes.JSON `gorm:"type:jsonb;not null" json:"sample_phase"`
	ReferenceAmplitude datatypes.JSON `gorm:"type:jsonb" json:"reference_amplitude,omitempty"`
	ReferencePhase    datatypes.JSON `gorm:"type:jsonb" json:"reference_phase,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

type OpticalParam struct {
	ID                string         `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	AnalysisID        string         `gorm:"type:uuid;not null" json:"analysis_id"`
	Frequencies       datatypes.JSON `gorm:"type:jsonb;not null" json:"frequencies"`
	AbsorptionCoeff   datatypes.JSON `gorm:"type:jsonb;not null" json:"absorption_coeff"`
	RefractiveIndex   datatypes.JSON `gorm:"type:jsonb;not null" json:"refractive_index"`
	ExtinctionCoeff   datatypes.JSON `gorm:"type:jsonb" json:"extinction_coeff,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

type WaveformData struct {
	Time          []float64 `json:"time"`
	SampleField   []float64 `json:"sample_field"`
	ReferenceField []float64 `json:"reference_field,omitempty"`
}

type TaskMessage struct {
	AnalysisID        string    `json:"analysis_id"`
	SampleName        string    `json:"sample_name"`
	MaterialType      string    `json:"material_type"`
	SampleThicknessMM float64   `json:"sample_thickness_mm"`
	Waveform          WaveformData `json:"waveform"`
	Timestamp         time.Time `json:"timestamp"`
}

type ProgressMessage struct {
	AnalysisID string         `json:"analysis_id"`
	Status     AnalysisStatus `json:"status"`
	Progress   int            `json:"progress"`
	Message    string         `json:"message"`
	Data       interface{}    `json:"data,omitempty"`
}

type BandInfo struct {
	Valid      bool    `json:"valid"`
	StartFreqHz float64 `json:"start_freq_hz"`
	EndFreqHz   float64 `json:"end_freq_hz"`
	StartIndex  int     `json:"start_index"`
	EndIndex    int     `json:"end_index"`
	NumPoints   int     `json:"num_points"`
	SnrMean     float64 `json:"snr_mean"`
	SnrMax      float64 `json:"snr_max"`
}

type FFTResult struct {
	Frequencies        []float64  `json:"frequencies"`
	SampleAmplitude    []float64  `json:"sample_amplitude"`
	SamplePhase        []float64  `json:"sample_phase"`
	ReferenceAmplitude []float64  `json:"reference_amplitude,omitempty"`
	ReferencePhase     []float64  `json:"reference_phase,omitempty"`
	BandInfo           *BandInfo  `json:"band_info,omitempty"`
	SpeedupRatio       float64    `json:"speedup_ratio"`
}

type ParamsResult struct {
	Frequencies     []float64 `json:"frequencies"`
	AbsorptionCoeff []float64 `json:"absorption_coeff"`
	RefractiveIndex []float64 `json:"refractive_index"`
	ExtinctionCoeff []float64 `json:"extinction_coeff,omitempty"`
	BandInfo        *BandInfo `json:"band_info,omitempty"`
}

type FinalResult struct {
	FFT     FFTResult  `json:"fft"`
	Params  ParamsResult `json:"params"`
	Moisture float64   `json:"moisture_content_percent"`
}

type AnomalyType struct {
	Bubble          bool `json:"bubble"`
	ThicknessUneven bool `json:"thickness_uneven"`
	LowSnr          bool `json:"low_snr"`
	Distorted       bool `json:"distorted"`
}

type AnomalyDetectionResult struct {
	IsInvalid     bool         `json:"is_invalid"`
	AnomalyScore  float64      `json:"anomaly_score"`
	Confidence    float64      `json:"confidence"`
	Reasons       []string     `json:"reasons"`
	AnomalyType   AnomalyType  `json:"anomaly_type"`
	Severity      string       `json:"severity"`
}

type PerformanceMetrics struct {
	FFTTimeMs             float64 `json:"fft_time_ms"`
	ParamsTimeMs          float64 `json:"params_time_ms"`
	PredictionTimeMs      float64 `json:"prediction_time_ms"`
	TotalTimeMs           float64 `json:"total_time_ms"`
	FFTSpeedup            float64 `json:"fft_speedup"`
	PredictionSpeedup     float64 `json:"prediction_speedup"`
	TotalSpeedup          float64 `json:"total_speedup"`
	PredictionProcessingTimeMs float64 `json:"prediction_processing_time_ms"`
	ValidSamplesProcessed int     `json:"valid_samples_processed"`
}

type WorkerResultMessage struct {
	AnalysisID       string                 `json:"analysis_id"`
	Status           AnalysisStatus         `json:"status"`
	Progress         int                    `json:"progress"`
	Stage            string                 `json:"stage"`
	FFT              *FFTResult             `json:"fft,omitempty"`
	Params           *ParamsResult          `json:"params,omitempty"`
	Moisture         *float64               `json:"moisture,omitempty"`
	Error            string                 `json:"error,omitempty"`
	Timestamp        time.Time              `json:"timestamp"`
	AnomalyDetection *AnomalyDetectionResult `json:"anomaly_detection,omitempty"`
	Performance      *PerformanceMetrics    `json:"performance,omitempty"`
}

type AnalysisDetail struct {
	Analysis          *Analysis         `json:"analysis"`
	RawWaveform       *RawWaveform      `json:"raw_waveform,omitempty"`
	FrequencySpectrum *FrequencySpectrum `json:"frequency_spectrum,omitempty"`
	OpticalParam      *OpticalParam     `json:"optical_params,omitempty"`
}

type UploadResponse struct {
	AnalysisID string `json:"analysis_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type DifferentialComparison struct {
	ID                      string         `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	MaterialType            string         `gorm:"not null" json:"material_type"`
	SampleThicknessMM       float64        `gorm:"not null" json:"sample_thickness_mm"`
	AnalysisID_T1           string         `gorm:"type:uuid;not null" json:"analysis_id_t1"`
	AnalysisID_T2           string         `gorm:"type:uuid;not null" json:"analysis_id_t2"`
	TimeIntervalHours       float64        `gorm:"not null" json:"time_interval_hours"`
	Status                  AnalysisStatus `gorm:"not null;default:pending" json:"status"`
	MigrationRate           *float64       `json:"migration_rate_per_hour,omitempty"`
	DeltaMoisture           *float64       `json:"delta_moisture,omitempty"`
	MoistureT1              *float64       `json:"moisture_t1,omitempty"`
	MoistureT2              *float64       `json:"moisture_t2,omitempty"`
	DifferenceSpectrum      datatypes.JSON `gorm:"type:jsonb" json:"difference_spectrum,omitempty"`
	DryingEfficiency        *float64       `json:"drying_efficiency,omitempty"`
	HalfLifeHours           *float64       `json:"half_life_hours,omitempty"`
	ErrorMessage            string         `json:"error_message,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
	CompletedAt             *time.Time     `json:"completed_at,omitempty"`
}

type DifferentialTask struct {
	ID                string         `json:"id"`
	MaterialType      string         `json:"material_type"`
	SampleThicknessMM float64        `json:"sample_thickness_mm"`
	TimeIntervalHours float64        `json:"time_interval_hours"`
	WaveformT1        WaveformData   `json:"waveform_t1"`
	WaveformT2        WaveformData   `json:"waveform_t2"`
	Timestamp         time.Time      `json:"timestamp"`
}

type DifferentialResult struct {
	MigrationRate        float64                `json:"migration_rate_per_hour"`
	DeltaMoisture        float64                `json:"delta_moisture"`
	MoistureT1           float64                `json:"moisture_t1"`
	MoistureT2           float64                `json:"moisture_t2"`
	DifferenceSpectrum   *DifferenceSpectrumData `json:"difference_spectrum"`
	DryingEfficiency    float64                `json:"drying_efficiency"`
	HalfLifeHours       *float64               `json:"half_life_hours,omitempty"`
	IsDrying            bool                   `json:"is_drying"`
}

type DifferenceSpectrumData struct {
	Frequencies        []float64 `json:"frequencies"`
	DeltaAbsorption    []float64 `json:"delta_absorption"`
	AbsorptionRatio    []float64 `json:"absorption_ratio"`
	DeltaRefractive    []float64 `json:"delta_refractive_index,omitempty"`
	MeanDeltaAlpha     float64   `json:"mean_delta_alpha"`
	MaxDeltaAlpha      float64   `json:"max_delta_alpha"`
	IntegratedDelta    float64   `json:"integrated_delta"`
}

type BatchUploadResult struct {
	AnalysisIDs    []string                 `json:"analysis_ids"`
	Status         string                   `json:"status"`
	TotalCount     int                      `json:"total_count"`
	SuccessCount   int                      `json:"success_count"`
	FailedCount    int                      `json:"failed_count"`
	DuplicateCount int                      `json:"duplicate_count"`
	Results        []BatchUploadItemResult `json:"results"`
}

type BatchUploadItemResult struct {
	Index          int      `json:"index"`
	FileName       string   `json:"file_name"`
	AnalysisID     string   `json:"analysis_id,omitempty"`
	Status         string   `json:"status"`
	Message        string   `json:"message"`
	IsDuplicate    bool     `json:"is_duplicate"`
	MD5            string   `json:"md5,omitempty"`
}

type DifferentialComparisonDetail struct {
	Comparison *DifferentialComparison   `json:"comparison"`
	AnalysisT1 *AnalysisDetail          `json:"analysis_t1,omitempty"`
	AnalysisT2 *AnalysisDetail          `json:"analysis_t2,omitempty"`
	Result     *DifferentialResult      `json:"result,omitempty"`
}

type CacheRecord struct {
	ID              string    `gorm:"primaryKey" json:"id"`
	MD5             string    `gorm:"uniqueIndex;not null" json:"md5"`
	AnalysisID      string    `gorm:"type:uuid;not null" json:"analysis_id"`
	TimePointsHash  string    `json:"time_points_hash"`
	SampleFieldHash string    `json:"sample_field_hash"`
	HitCount        int       `gorm:"default:0" json:"hit_count"`
	LastAccessedAt  time.Time `json:"last_accessed_at"`
	CreatedAt       time.Time `json:"created_at"`
}

type MetricsResponse struct {
	TaskQueueDepth        float64 `json:"task_queue_depth"`
	ResultQueueDepth      float64 `json:"result_queue_depth"`
	ActiveWorkers         float64 `json:"active_workers"`
	TasksTotal            float64 `json:"tasks_total"`
	TasksCompleted        float64 `json:"tasks_completed_total"`
	TasksFailed           float64 `json:"tasks_failed_total"`
	TasksInvalid          float64 `json:"tasks_invalid_total"`
	CacheHits             float64 `json:"cache_hits_total"`
	CacheMisses           float64 `json:"cache_misses_total"`
	CacheHitRate          float64 `json:"cache_hit_rate_percent"`
	AverageFFTDuration    float64 `json:"average_fft_duration_seconds"`
	AverageParamsDuration float64 `json:"average_params_duration_seconds"`
	AveragePredictionDuration float64 `json:"average_prediction_duration_seconds"`
}

type WorkerMetricMessage struct {
	AnalysisID       string  `json:"analysis_id"`
	Stage            string  `json:"stage"`
	DurationSeconds  float64 `json:"duration_seconds"`
	Success          bool    `json:"success"`
	QueueDepth       int     `json:"queue_depth,omitempty"`
	ActiveWorkers    int     `json:"active_workers,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
}
