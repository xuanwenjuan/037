CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS analyses (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sample_name VARCHAR(255) NOT NULL,
    material_type VARCHAR(100),
    sample_thickness_mm FLOAT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE,
    moisture_content_percent FLOAT,
    is_valid BOOLEAN DEFAULT TRUE,
    anomaly_score FLOAT,
    anomaly_confidence FLOAT,
    anomaly_severity VARCHAR(20),
    anomaly_reasons JSONB,
    anomaly_types JSONB,
    band_start_freq_thz FLOAT,
    band_end_freq_thz FLOAT,
    total_speedup_ratio FLOAT,
    prediction_time_ms FLOAT
);

CREATE TABLE IF NOT EXISTS raw_waveforms (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    analysis_id UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    time_points JSONB NOT NULL,
    sample_field JSONB NOT NULL,
    reference_field JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS frequency_spectra (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    analysis_id UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    frequencies JSONB NOT NULL,
    sample_amplitude JSONB NOT NULL,
    sample_phase JSONB NOT NULL,
    reference_amplitude JSONB,
    reference_phase JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS optical_params (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    analysis_id UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    frequencies JSONB NOT NULL,
    absorption_coeff JSONB NOT NULL,
    refractive_index JSONB NOT NULL,
    extinction_coeff JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_analyses_status ON analyses(status);
CREATE INDEX IF NOT EXISTS idx_analyses_created ON analyses(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_analyses_is_valid ON analyses(is_valid);
CREATE INDEX IF NOT EXISTS idx_analyses_anomaly_severity ON analyses(anomaly_severity);
CREATE INDEX IF NOT EXISTS idx_waveforms_analysis ON raw_waveforms(analysis_id);
CREATE INDEX IF NOT EXISTS idx_spectra_analysis ON frequency_spectra(analysis_id);
CREATE INDEX IF NOT EXISTS idx_params_analysis ON optical_params(analysis_id);

CREATE TABLE IF NOT EXISTS differential_comparisons (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    material_type VARCHAR(100) NOT NULL,
    sample_thickness_mm FLOAT NOT NULL,
    analysis_id_t1 UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    analysis_id_t2 UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    time_interval_hours FLOAT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    migration_rate_per_hour FLOAT,
    delta_moisture FLOAT,
    moisture_t1 FLOAT,
    moisture_t2 FLOAT,
    difference_spectrum JSONB,
    drying_efficiency FLOAT,
    half_life_hours FLOAT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS cache_records (
    id VARCHAR(64) PRIMARY KEY,
    md5 VARCHAR(32) UNIQUE NOT NULL,
    analysis_id UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    time_points_hash VARCHAR(64) NOT NULL,
    sample_field_hash VARCHAR(64) NOT NULL,
    hit_count INTEGER DEFAULT 0,
    last_accessed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS metric_records (
    id BIGSERIAL PRIMARY KEY,
    metric_name VARCHAR(100) NOT NULL,
    metric_value FLOAT NOT NULL,
    labels JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_diff_comparisons_status ON differential_comparisons(status);
CREATE INDEX IF NOT EXISTS idx_diff_comparisons_material ON differential_comparisons(material_type);
CREATE INDEX IF NOT EXISTS idx_diff_comparisons_created ON differential_comparisons(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cache_md5 ON cache_records(md5);
CREATE INDEX IF NOT EXISTS idx_cache_analysis ON cache_records(analysis_id);
CREATE INDEX IF NOT EXISTS idx_metrics_name ON metric_records(metric_name);
CREATE INDEX IF NOT EXISTS idx_metrics_created ON metric_records(created_at DESC);
