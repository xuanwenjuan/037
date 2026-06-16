package models

import (
	"time"

	"gorm.io/datatypes"
)

type AnalysisStatus string

const (
	StatusPending   AnalysisStatus = "pending"
	StatusQueued    AnalysisStatus = "queued"
	StatusProcessing AnalysisStatus = "processing"
	StatusFFTDone   AnalysisStatus = "fft_done"
	StatusParamsDone AnalysisStatus = "params_done"
	StatusCompleted AnalysisStatus = "completed"
	StatusFailed    AnalysisStatus = "failed"
)

type Analysis struct {
	ID                   string         `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	SampleName           string         `gorm:"not null" json:"sample_name"`
	MaterialType         string         `json:"material_type"`
	SampleThicknessMM    float64        `gorm:"not null" json:"sample_thickness_mm"`
	Status               AnalysisStatus `gorm:"not null;default:pending" json:"status"`
	ErrorMessage         string         `json:"error_message,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	CompletedAt          *time.Time     `json:"completed_at,omitempty"`
	MoistureContentPercent *float64      `json:"moisture_content_percent,omitempty"`
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

type FFTResult struct {
	Frequencies       []float64 `json:"frequencies"`
	SampleAmplitude   []float64 `json:"sample_amplitude"`
	SamplePhase       []float64 `json:"sample_phase"`
	ReferenceAmplitude []float64 `json:"reference_amplitude,omitempty"`
	ReferencePhase    []float64 `json:"reference_phase,omitempty"`
}

type ParamsResult struct {
	Frequencies     []float64 `json:"frequencies"`
	AbsorptionCoeff []float64 `json:"absorption_coeff"`
	RefractiveIndex []float64 `json:"refractive_index"`
	ExtinctionCoeff []float64 `json:"extinction_coeff,omitempty"`
}

type FinalResult struct {
	FFT     FFTResult  `json:"fft"`
	Params  ParamsResult `json:"params"`
	Moisture float64   `json:"moisture_content_percent"`
}

type WorkerResultMessage struct {
	AnalysisID string       `json:"analysis_id"`
	Status     AnalysisStatus `json:"status"`
	Progress   int          `json:"progress"`
	Stage      string       `json:"stage"`
	FFT        *FFTResult   `json:"fft,omitempty"`
	Params     *ParamsResult `json:"params,omitempty"`
	Moisture   *float64     `json:"moisture,omitempty"`
	Error      string       `json:"error,omitempty"`
	Timestamp  time.Time    `json:"timestamp"`
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
