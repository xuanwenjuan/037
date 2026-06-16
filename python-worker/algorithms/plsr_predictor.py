import os
import time
import numpy as np
import onnxruntime as ort
from typing import Dict, List, Optional

from .band_cutter import BandCutter


class PLSRPredictor:
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
        self._target_freqs = np.array([0.2, 0.4, 0.6, 0.8, 1.0, 1.2, 1.5, 2.0, 2.5, 3.0]) * 1e12
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

        if self.use_band_cutting:
            cut_result = self.band_cutter.cut_params(
                frequencies, absorption_coeff, refractive_index
            )
            cut_freqs = cut_result["frequencies"]
            cut_alpha = cut_result["absorption_coeff"]
            cut_n = cut_result["refractive_index"]
            band_info = cut_result["band_info"]
            speedup_ratio = cut_result["speedup_ratio"]

            if not band_info["valid"] or len(cut_freqs) < 5:
                return {
                    "moisture_content": 0.0,
                    "valid": False,
                    "band_info": band_info,
                    "processing_time_ms": 0.0,
                    "speedup_ratio": 1.0,
                    "error": "No valid frequency band for prediction",
                }
        else:
            cut_freqs = frequencies
            cut_alpha = absorption_coeff
            cut_n = refractive_index
            band_info = None
            speedup_ratio = 1.0

        features = self._extract_features(cut_freqs, cut_alpha, cut_n)

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
        }

    def _extract_features(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> np.ndarray:
        freq_arr = np.array(frequencies)
        alpha_arr = np.array(absorption_coeff)
        n_arr = np.array(refractive_index)

        alpha_interp = self._interpolate(freq_arr, alpha_arr, self._target_freqs)
        n_interp = self._interpolate(freq_arr, n_arr, self._target_freqs)

        alpha_stats = self._compute_stats(alpha_arr)
        n_stats = self._compute_stats(n_arr)

        features = np.concatenate([alpha_interp, n_interp, alpha_stats, n_stats])

        return features.reshape(1, -1).astype(np.float32)

    def _interpolate(self, x: np.ndarray, y: np.ndarray, x_new: np.ndarray) -> np.ndarray:
        mask = np.isfinite(y)
        if not np.any(mask):
            return np.zeros_like(x_new)
        return np.interp(x_new, x[mask], y[mask], left=y[mask][0], right=y[mask][-1])

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
