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
    moisture_content_percent FLOAT
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
CREATE INDEX IF NOT EXISTS idx_waveforms_analysis ON raw_waveforms(analysis_id);
CREATE INDEX IF NOT EXISTS idx_spectra_analysis ON frequency_spectra(analysis_id);
CREATE INDEX IF NOT EXISTS idx_params_analysis ON optical_params(analysis_id);
