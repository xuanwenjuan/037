import os
import sys
import json
import time
from datetime import datetime
from typing import Dict, List, Optional

import pika
from celery import Celery
from config import config

from algorithms import FFTProcessor, DorneyDuvillaret, PLSRPredictor

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
plsr_predictor = PLSRPredictor(config.ONNX_MODEL_PATH)


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
    analysis_id = task_data["analysis_id"]
    waveform = task_data["waveform"]
    sample_thickness = task_data["sample_thickness_mm"]

    try:
        send_progress(
            analysis_id,
            "processing",
            5,
            "Starting waveform processing",
        )

        time.sleep(0.1)

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

        fft_output = {
            "frequencies": fft_result["frequencies"],
            "sample_amplitude": fft_result["sample_amplitude"],
            "sample_phase": fft_result["sample_phase"],
            "reference_amplitude": fft_result.get("reference_amplitude"),
            "reference_phase": fft_result.get("reference_phase"),
        }

        send_progress(
            analysis_id,
            "fft_done",
            40,
            "FFT transformation completed",
            fft=fft_output,
        )

        send_progress(
            analysis_id,
            "processing",
            45,
            "Extracting optical parameters",
        )

        params_result = dorney.extract_parameters(
            frequencies=fft_result["frequencies"],
            sample_amp=fft_result["sample_amplitude"],
            sample_phase=fft_result["sample_phase"],
            reference_amp=fft_result.get("reference_amplitude"),
            reference_phase=fft_result.get("reference_phase"),
            sample_thickness_mm=sample_thickness,
        )

        params_output = {
            "frequencies": params_result["frequencies"],
            "absorption_coeff": params_result["absorption_coeff"],
            "refractive_index": params_result["refractive_index"],
            "extinction_coeff": params_result.get("extinction_coeff"),
        }

        send_progress(
            analysis_id,
            "params_done",
            75,
            "Optical parameters extracted",
            params=params_output,
        )

        send_progress(
            analysis_id,
            "processing",
            80,
            "Predicting moisture content",
        )

        moisture = plsr_predictor.predict(
            frequencies=params_result["frequencies"],
            absorption_coeff=params_result["absorption_coeff"],
            refractive_index=params_result["refractive_index"],
        )

        send_progress(
            analysis_id,
            "completed",
            100,
            "Analysis completed successfully",
            moisture=moisture,
        )

        return {
            "analysis_id": analysis_id,
            "status": "completed",
            "fft": fft_output,
            "params": params_output,
            "moisture_content_percent": moisture,
        }

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
