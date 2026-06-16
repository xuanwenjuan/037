import os
import sys
import json
import time
from datetime import datetime
from typing import Dict, List, Optional

import pika
from celery import Celery
from config import config

from algorithms import FFTProcessor, DorneyDuvillaret, PLSRPredictor, BandCutter, AnomalyDetector

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

app = Celery(
    "thz_tasks",
    broker=config.CELERY_BROKER_URL,
    backend=config.CELERY_RESULT_BACKEND,
)

app.conf.update(
    task_serializer="json",
    accept_content=["json"],
    result_serializer="json",
    timezone="UTC",
    enable_utc=True,
    task_publish_retry=True,
    task_publish_retry_policy={
        "max_retries": 3,
        "interval_start": 0,
        "interval_step": 0.2,
        "interval_max": 0.5,
    },
)

fft_processor = FFTProcessor()
dorney = DorneyDuvillaret()
plsr_predictor = PLSRPredictor(config.ONNX_MODEL_PATH, use_band_cutting=True)
band_cutter = BandCutter(min_freq_thz=0.1, max_freq_thz=4.0, snr_threshold=3.0)
anomaly_detector = AnomalyDetector(contamination=0.1)
_valid_sample_count = 0


def send_result(result_data: Dict) -> None:
    try:
        connection = pika.BlockingConnection(pika.URLParameters(config.RABBITMQ_URL))
        channel = connection.channel()

        channel.queue_declare(queue=config.RESULT_QUEUE, durable=True)

        channel.basic_publish(
            exchange="",
            routing_key=config.RESULT_QUEUE,
            body=json.dumps(result_data),
            properties=pika.BasicProperties(
                delivery_mode=2,
                content_type="application/json",
            ),
        )

        connection.close()
    except Exception as e:
        print(f"Failed to send result: {e}")


def send_progress(
    analysis_id: str,
    status: str,
    progress: int,
    stage: str,
    fft: Optional[Dict] = None,
    params: Optional[Dict] = None,
    moisture: Optional[float] = None,
    error: Optional[str] = None,
) -> None:
    result = {
        "analysis_id": analysis_id,
        "status": status,
        "progress": progress,
        "stage": stage,
        "timestamp": datetime.utcnow().isoformat(),
    }

    if fft is not None:
        result["fft"] = fft
    if params is not None:
        result["params"] = params
    if moisture is not None:
        result["moisture"] = moisture
    if error is not None:
        result["error"] = error

    send_result(result)


@app.task(name="process_thz_waveform", bind=True, max_retries=3)
def process_thz_waveform(self, task_data: Dict) -> Dict:
    global _valid_sample_count

    analysis_id = task_data["analysis_id"]
    waveform = task_data["waveform"]
    sample_thickness = task_data["sample_thickness_mm"]

    try:
        send_progress(
            analysis_id,
            "processing",
            3,
            "Starting waveform quality check",
        )

        anomaly_result = anomaly_detector.detect_anomaly(
            time=waveform["time"],
            field=waveform["sample_field"],
            sample_thickness_mm=sample_thickness,
        )

        send_progress(
            analysis_id,
            "processing",
            5,
            "Waveform quality check completed",
        )

        send_progress(
            analysis_id,
            "processing",
            10,
            "Performing FFT transformation",
        )

        fft_result = fft_processor.process_waveform(
            time=waveform["time"],
            sample_field=waveform["sample_field"],
            reference_field=waveform.get("reference_field"),
        )

        send_progress(
            analysis_id,
            "processing",
            20,
            "Performing intelligent band cutting",
        )

        cut_spectrum = band_cutter.cut_spectrum(
            frequencies=fft_result["frequencies"],
            sample_amp=fft_result["sample_amplitude"],
            sample_phase=fft_result["sample_phase"],
            reference_amp=fft_result.get("reference_amplitude"),
            reference_phase=fft_result.get("reference_phase"),
        )

        band_info = cut_spectrum["band_info"]
        speedup_fft = cut_spectrum["speedup_ratio"]

        fft_output = {
            "frequencies": cut_spectrum["frequencies"],
            "sample_amplitude": cut_spectrum["sample_amplitude"],
            "sample_phase": cut_spectrum["sample_phase"],
            "reference_amplitude": cut_spectrum.get("reference_amplitude"),
            "reference_phase": cut_spectrum.get("reference_phase"),
            "band_info": band_info,
            "speedup_ratio": float(speedup_fft),
        }

        send_progress(
            analysis_id,
            "fft_done",
            40,
            f"FFT transformation completed, valid band: {band_info.get('start_freq_hz', 0)/1e12:.2f}-{band_info.get('end_freq_hz', 0)/1e12:.2f} THz",
            fft=fft_output,
        )

        send_progress(
            analysis_id,
            "processing",
            45,
            "Extracting optical parameters",
        )

        params_result = dorney.extract_parameters(
            frequencies=cut_spectrum["frequencies"],
            sample_amp=cut_spectrum["sample_amplitude"],
            sample_phase=cut_spectrum["sample_phase"],
            reference_amp=cut_spectrum.get("reference_amplitude"),
            reference_phase=cut_spectrum.get("reference_phase"),
            sample_thickness_mm=sample_thickness,
        )

        params_output = {
            "frequencies": params_result["frequencies"],
            "absorption_coeff": params_result["absorption_coeff"],
            "refractive_index": params_result["refractive_index"],
            "extinction_coeff": params_result.get("extinction_coeff"),
            "band_info": band_info,
        }

        send_progress(
            analysis_id,
            "params_done",
            75,
            "Optical parameters extracted",
            params=params_output,
        )

        anomaly_result = anomaly_detector.detect_anomaly(
            time=waveform["time"],
            field=waveform["sample_field"],
            frequencies=params_result["frequencies"],
            amplitude=cut_spectrum["sample_amplitude"],
            alpha=params_result["absorption_coeff"],
            n=params_result["refractive_index"],
            sample_thickness_mm=sample_thickness,
        )

        if anomaly_result["is_invalid"]:
            print(f"Task {analysis_id}: Invalid sample detected - {anomaly_result['reasons']}")

            invalid_result = {
                "analysis_id": analysis_id,
                "status": "invalid",
                "progress": 100,
                "stage": "Invalid sample detected",
                "anomaly_detection": {
                    "is_invalid": True,
                    "anomaly_score": anomaly_result["anomaly_score"],
                    "confidence": anomaly_result["confidence"],
                    "reasons": anomaly_result["reasons"],
                    "anomaly_type": anomaly_result["anomaly_type"],
                    "severity": anomaly_result["severity"],
                },
                "fft": fft_output,
                "params": params_output,
                "timestamp": datetime.utcnow().isoformat(),
            }
            send_result(invalid_result)
            return invalid_result

        anomaly_detector.update_reference(anomaly_result["features"])
        _valid_sample_count += 1

        send_progress(
            analysis_id,
            "processing",
            80,
            "Predicting moisture content",
        )

        prediction = plsr_predictor.predict_with_details(
            frequencies=params_result["frequencies"],
            absorption_coeff=params_result["absorption_coeff"],
            refractive_index=params_result["refractive_index"],
        )

        moisture = prediction["moisture_content"]
        total_speedup = prediction.get("speedup_ratio", 1.0) * speedup_fft

        send_progress(
            analysis_id,
            "completed",
            100,
            f"Analysis completed successfully, speedup: {total_speedup:.1f}x",
            moisture=moisture,
        )

        final_result = {
            "analysis_id": analysis_id,
            "status": "completed",
            "fft": fft_output,
            "params": params_output,
            "moisture_content_percent": moisture,
            "anomaly_detection": {
                "is_invalid": False,
                "anomaly_score": anomaly_result["anomaly_score"],
                "confidence": anomaly_result["confidence"],
                "anomaly_type": anomaly_result["anomaly_type"],
                "severity": anomaly_result["severity"],
            },
            "performance": {
                "fft_speedup": float(speedup_fft),
                "prediction_speedup": float(prediction.get("speedup_ratio", 1.0)),
                "total_speedup": float(total_speedup),
                "prediction_time_ms": float(prediction.get("processing_time_ms", 0)),
                "valid_samples_processed": _valid_sample_count,
            },
        }

        send_result(final_result)

        print(f"Task {analysis_id} completed: moisture = {moisture:.4f}%, speedup = {total_speedup:.1f}x")

        return final_result

    except Exception as e:
        print(f"Error processing task {analysis_id}: {e}")
        import traceback
        traceback.print_exc()

        send_progress(
            analysis_id,
            "failed",
            0,
            "Analysis failed",
            error=str(e),
        )

        self.retry(exc=e, countdown=30)

        return {
            "analysis_id": analysis_id,
            "status": "failed",
            "error": str(e),
        }


@app.task(name="fft_task")
def fft_task(waveform: Dict) -> Dict:
    return fft_processor.process_waveform(
        time=waveform["time"],
        sample_field=waveform["sample_field"],
        reference_field=waveform.get("reference_field"),
    )


@app.task(name="extract_params_task")
def extract_params_task(fft_data: Dict, sample_thickness_mm: float) -> Dict:
    return dorney.extract_parameters(
        frequencies=fft_data["frequencies"],
        sample_amp=fft_data["sample_amplitude"],
        sample_phase=fft_data["sample_phase"],
        reference_amp=fft_data.get("reference_amplitude"),
        reference_phase=fft_data.get("reference_phase"),
        sample_thickness_mm=sample_thickness_mm,
    )


@app.task(name="predict_moisture_task")
def predict_moisture_task(params_data: Dict) -> float:
    return plsr_predictor.predict(
        frequencies=params_data["frequencies"],
        absorption_coeff=params_data["absorption_coeff"],
        refractive_index=params_data["refractive_index"],
    )


if __name__ == "__main__":
    app.start()
