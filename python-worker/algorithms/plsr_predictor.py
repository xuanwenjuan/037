import os
import time
import numpy as np
import onnxruntime as ort
from typing import Dict, List, Optional

from .band_cutter import BandCutter
from .sinc_resampler import SincResampler


class PLSRPredictor:
    STANDARD_FREQ_MIN_THZ = 0.1
    STANDARD_FREQ_MAX_THZ = 4.0
    STANDARD_FREQ_STEP_THZ = 0.01

    def __init__(self, model_path: str, use_band_cutting: bool = True):
        self.model_path = model_path
        self.session = None
        self.input_name = None
        self.output_name = None
        self.use_band_cutting = use_band_cutting
        self.band_cutter = BandCutter(
            min_freq_thz=0.1,
            max_freq_thz=4.0,
            snr_threshold=3.0,
        )
        self.resampler = SincResampler(
            freq_min_thz=self.STANDARD_FREQ_MIN_THZ,
            freq_max_thz=self.STANDARD_FREQ_MAX_THZ,
            freq_step_thz=self.STANDARD_FREQ_STEP_THZ,
        )
        self._standard_freqs = self.resampler.standard_freqs_hz
        self._num_standard_points = self.resampler.num_standard_points
        self._target_freqs = self._standard_freqs
        self._load_model()

    def _load_model(self) -> None:
        if not os.path.exists(self.model_path):
            print(f"Warning: ONNX model not found at {self.model_path}")
            print("Using fallback prediction model...")
            self.session = None
            return

        try:
            providers = ["CPUExecutionProvider"]
            self.session = ort.InferenceSession(self.model_path, providers=providers)

            input_meta = self.session.get_inputs()
            output_meta = self.session.get_outputs()

            if len(input_meta) > 0:
                self.input_name = input_meta[0].name
            if len(output_meta) > 0:
                self.output_name = output_meta[0].name

            print(f"PLSR model loaded successfully from {self.model_path}")
        except Exception as e:
            print(f"Failed to load ONNX model: {e}")
            print("Using fallback prediction model...")
            self.session = None

    def predict(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> float:
        result = self.predict_with_details(
            frequencies, absorption_coeff, refractive_index
        )
        return result["moisture_content"]

    def predict_with_details(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> Dict:
        start_time = time.time()

        resampled = self.resampler.resample_optical_params(
            frequencies, absorption_coeff, refractive_index
        )
        resample_info = resampled["resample_info"]

        cut_freqs = resampled["frequencies"]
        cut_alpha = resampled["absorption_coeff"]
        cut_n = resampled["refractive_index"]
        band_info = {
            "valid": True,
            "resampled": True,
            "standard_freq_min_thz": self.STANDARD_FREQ_MIN_THZ,
            "standard_freq_max_thz": self.STANDARD_FREQ_MAX_THZ,
            "standard_freq_step_thz": self.STANDARD_FREQ_STEP_THZ,
            "standard_num_points": self._num_standard_points,
        }
        speedup_ratio = 1.0

        if self.use_band_cutting:
            cut_result = self.band_cutter.cut_params(
                cut_freqs, cut_alpha, cut_n
            )
            if cut_result["band_info"]["valid"] and len(cut_result["frequencies"]) >= 5:
                cut_freqs = cut_result["frequencies"]
                cut_alpha = cut_result["absorption_coeff"]
                cut_n = cut_result["refractive_index"]
                band_info = cut_result["band_info"]
                band_info["resampled"] = True
                speedup_ratio = cut_result["speedup_ratio"]
            else:
                band_info["band_cutting_skipped"] = True
                band_info["band_cutting_reason"] = "insufficient_valid_points_after_resampling"

        alpha_arr = np.array(cut_alpha, dtype=np.float64)
        n_arr = np.array(cut_n, dtype=np.float64)

        if len(alpha_arr) < 3 or not np.any(np.isfinite(alpha_arr)):
            return {
                "moisture_content": 0.0,
                "valid": False,
                "band_info": band_info,
                "processing_time_ms": 0.0,
                "speedup_ratio": speedup_ratio,
                "error": "No valid spectral data after resampling",
                "resample_info": resample_info,
            }

        features = self._extract_features_from_resampled(alpha_arr, n_arr)

        if self.session is not None:
            moisture = self._predict_onnx(features)
        else:
            moisture = self._predict_fallback_from_features(features)

        processing_time = (time.time() - start_time) * 1000

        return {
            "moisture_content": moisture,
            "valid": True,
            "band_info": band_info,
            "processing_time_ms": processing_time,
            "speedup_ratio": speedup_ratio,
            "feature_dim": features.shape[1],
            "resample_info": resample_info,
        }

    def _extract_features_from_resampled(
        self,
        alpha_arr: np.ndarray,
        n_arr: np.ndarray,
    ) -> np.ndarray:
        alpha_interp = self._resample_to_target(alpha_arr)
        n_interp = self._resample_to_target(n_arr)

        alpha_stats = self._compute_stats(alpha_arr)
        n_stats = self._compute_stats(n_arr)

        features = np.concatenate([alpha_interp, n_interp, alpha_stats, n_stats])

        return features.reshape(1, -1).astype(np.float32)

    def _resample_to_target(self, values: np.ndarray) -> np.ndarray:
        if len(values) == self._num_standard_points:
            mask = np.isfinite(values)
            result = values.copy()
            result[~mask] = 0.0
            return result

        if len(values) == 0:
            return np.zeros(self._num_standard_points)

        mask = np.isfinite(values)
        if not np.any(mask):
            return np.zeros(self._num_standard_points)

        valid_vals = values[mask]
        step = max(1, len(valid_vals) // self._num_standard_points)
        sampled = valid_vals[::step][:self._num_standard_points]

        if len(sampled) < self._num_standard_points:
            padded = np.zeros(self._num_standard_points)
            padded[:len(sampled)] = sampled
            return padded

        return sampled

    def _extract_features(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> np.ndarray:
        resampled = self.resampler.resample_optical_params(
            frequencies, absorption_coeff, refractive_index
        )
        alpha_arr = np.array(resampled["absorption_coeff"], dtype=np.float64)
        n_arr = np.array(resampled["refractive_index"], dtype=np.float64)
        return self._extract_features_from_resampled(alpha_arr, n_arr)

    def _compute_stats(self, arr: np.ndarray) -> np.ndarray:
        valid = arr[np.isfinite(arr)]
        if len(valid) == 0:
            return np.zeros(5)
        return np.array(
            [
                np.mean(valid),
                np.std(valid),
                np.max(valid),
                np.min(valid),
                np.median(valid),
            ]
        )

    def _predict_onnx(self, features: np.ndarray) -> float:
        try:
            inputs = {self.input_name: features}
            outputs = self.session.run([self.output_name], inputs)
            prediction = float(outputs[0][0][0])
            return max(0.0, min(100.0, prediction))
        except Exception as e:
            print(f"ONNX prediction failed: {e}")
            return self._predict_fallback_from_features(features)

    def _predict_fallback(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> float:
        features = self._extract_features(frequencies, absorption_coeff, refractive_index)
        return self._predict_fallback_from_features(features)

    def _predict_fallback_from_features(self, features: np.ndarray) -> float:
        feat = features.flatten()

        alpha_mean = feat[20]
        alpha_std = feat[21]
        n_mean = feat[25]
        n_std = feat[26]

        alpha_interp = feat[0:10]

        weights = np.array([8.0, 7.5, 6.0, 4.5, 3.0, 2.5, 2.0, 1.5, 1.0, 0.8])
        weighted_alpha = np.sum(alpha_interp * weights) / np.sum(weights)

        moisture = (
            1.5 * weighted_alpha
            + 0.8 * alpha_mean
            + 1.2 * alpha_std
            - 2.5 * (n_mean - 1.5)
            + 5.0 * n_std
        )

        moisture = 0.5 * moisture + 3.0
        moisture += np.random.normal(0, 0.5)

        return float(max(0.0, min(100.0, moisture)))

    def predict_batch(
        self,
        frequencies_list: List[List[float]],
        alpha_list: List[List[float]],
        n_list: List[List[float]],
    ) -> List[float]:
        results = []
        for freqs, alpha, n in zip(frequencies_list, alpha_list, n_list):
            results.append(self.predict(freqs, alpha, n))
        return results

    def get_sensitive_bands(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
        num_bands: int = 5,
    ) -> List[Dict]:
        return self.band_cutter.select_sensitive_bands(
            frequencies, absorption_coeff, refractive_index, num_bands=num_bands
        )

    def get_band_info(
        self,
        frequencies: List[float],
        amplitude: List[float],
        reference_amplitude: Optional[List[float]] = None,
    ) -> Dict:
        return self.band_cutter.find_valid_band(
            frequencies, amplitude, reference_amplitude
        )
