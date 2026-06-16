import numpy as np
from typing import Dict, List, Optional, Tuple
from collections import deque
import math


class AnomalyDetector:
    def __init__(
        self,
        contamination: float = 0.1,
        n_estimators: int = 100,
        max_samples: int = 256,
        random_state: int = 42,
        use_sklearn_if_available: bool = True,
    ):
        self.contamination = contamination
        self.n_estimators = n_estimators
        self.max_samples = max_samples
        self.random_state = random_state
        self.rng = np.random.RandomState(random_state)

        self._sklearn_available = False
        self._isolation_forest = None

        if use_sklearn_if_available:
            try:
                from sklearn.ensemble import IsolationForest

                self._sklearn_available = True
                self._isolation_forest = IsolationForest(
                    n_estimators=n_estimators,
                    max_samples=max_samples,
                    contamination=contamination,
                    random_state=random_state,
                )
            except ImportError:
                self._sklearn_available = False

        self._reference_stats = None
        self._history_buffers = {
            "peak_amplitude": deque(maxlen=1000),
            "peak_position": deque(maxlen=1000),
            "pulse_width": deque(maxlen=1000),
            "rise_time": deque(maxlen=1000),
            "snr": deque(maxlen=1000),
        }
        self._fitted = False

    def extract_time_domain_features(
        self, time: List[float], field: List[float]
    ) -> Dict[str, float]:
        time_arr = np.array(time, dtype=np.float64)
        field_arr = np.array(field, dtype=np.float64)

        n = len(field_arr)
        if n < 10:
            return {
                "peak_amplitude": 0.0,
                "peak_position": 0.0,
                "peak_index": 0,
                "pulse_width": 0.0,
                "rise_time": 0.0,
                "fall_time": 0.0,
                "rms": 0.0,
                "mean": 0.0,
                "std": 0.0,
                "skewness": 0.0,
                "kurtosis": 0.0,
                "snr_estimate": 0.0,
                "num_peaks": 0,
                "max_slope": 0.0,
                "min_slope": 0.0,
            }

        peak_idx = int(np.argmax(np.abs(field_arr)))
        peak_amp = float(field_arr[peak_idx])
        peak_pos = float(time_arr[peak_idx])

        rms = float(np.sqrt(np.mean(field_arr ** 2)))
        mean_val = float(np.mean(field_arr))
        std_val = float(np.std(field_arr))

        if std_val > 1e-10:
            skewness = float(np.mean(((field_arr - mean_val) / std_val) ** 3))
            kurtosis = float(np.mean(((field_arr - mean_val) / std_val) ** 4) - 3)
        else:
            skewness = 0.0
            kurtosis = 0.0

        noise_mask = np.abs(field_arr) < std_val * 0.1
        noise_std = np.std(field_arr[noise_mask]) if np.any(noise_mask) else std_val * 0.1
        snr_estimate = float(abs(peak_amp) / noise_std) if noise_std > 1e-10 else 100.0

        threshold = peak_amp * 0.1
        above_thresh = np.where(np.abs(field_arr) >= abs(threshold))[0]

        if len(above_thresh) > 0:
            start_idx = above_thresh[0]
            end_idx = above_thresh[-1]
            pulse_width = float(time_arr[end_idx] - time_arr[start_idx])

            half_peak = peak_amp * 0.5
            rising_idx = np.where((np.arange(n) <= peak_idx) & (np.abs(field_arr) >= abs(half_peak)))[0]
            falling_idx = np.where((np.arange(n) >= peak_idx) & (np.abs(field_arr) >= abs(half_peak)))[0]

            rise_time = float(time_arr[peak_idx] - time_arr[rising_idx[0]]) if len(rising_idx) > 0 else 0.0
            fall_time = float(time_arr[falling_idx[-1]] - time_arr[peak_idx]) if len(falling_idx) > 0 else 0.0
        else:
            pulse_width = 0.0
            rise_time = 0.0
            fall_time = 0.0

        slopes = np.diff(field_arr)
        max_slope = float(np.max(slopes)) if len(slopes) > 0 else 0.0
        min_slope = float(np.min(slopes)) if len(slopes) > 0 else 0.0

        num_peaks = self._count_peaks(field_arr)

        return {
            "peak_amplitude": peak_amp,
            "peak_position": peak_pos,
            "peak_index": peak_idx,
            "pulse_width": pulse_width,
            "rise_time": rise_time,
            "fall_time": fall_time,
            "rms": rms,
            "mean": mean_val,
            "std": std_val,
            "skewness": skewness,
            "kurtosis": kurtosis,
            "snr_estimate": snr_estimate,
            "num_peaks": num_peaks,
            "max_slope": max_slope,
            "min_slope": min_slope,
        }

    def _count_peaks(self, signal: np.ndarray) -> int:
        if len(signal) < 3:
            return 0
        peaks = 0
        for i in range(1, len(signal) - 1):
            if signal[i] > signal[i - 1] and signal[i] > signal[i + 1]:
                peaks += 1
        return peaks

    def extract_freq_domain_features(
        self,
        frequencies: List[float],
        amplitude: List[float],
    ) -> Dict[str, float]:
        freq_arr = np.array(frequencies, dtype=np.float64)
        amp_arr = np.array(amplitude, dtype=np.float64)

        n = len(amp_arr)
        if n < 10:
            return {
                "spectral_centroid": 0.0,
                "spectral_bandwidth": 0.0,
                "spectral_rolloff": 0.0,
                "spectral_flatness": 0.0,
                "spectral_crest": 0.0,
                "max_amp": 0.0,
                "mean_amp": 0.0,
                "std_amp": 0.0,
            }

        total_energy = np.sum(amp_arr ** 2)
        if total_energy > 1e-10:
            spectral_centroid = float(np.sum(freq_arr * amp_arr ** 2) / total_energy)
        else:
            spectral_centroid = 0.0

        if spectral_centroid > 1e-10 and total_energy > 1e-10:
            spectral_bandwidth = float(
                np.sqrt(np.sum(((freq_arr - spectral_centroid) ** 2) * (amp_arr ** 2)) / total_energy)
            )
        else:
            spectral_bandwidth = 0.0

        cumulative_energy = np.cumsum(amp_arr ** 2)
        rolloff_threshold = 0.85 * total_energy
        rolloff_idx = np.where(cumulative_energy >= rolloff_threshold)[0]
        spectral_rolloff = float(freq_arr[rolloff_idx[0]]) if len(rolloff_idx) > 0 else 0.0

        geometric_mean = np.exp(np.mean(np.log(amp_arr + 1e-10)))
        arithmetic_mean = np.mean(amp_arr)
        spectral_flatness = float(geometric_mean / arithmetic_mean) if arithmetic_mean > 1e-10 else 0.0

        spectral_crest = float(np.max(amp_arr) / np.mean(amp_arr)) if np.mean(amp_arr) > 1e-10 else 0.0

        return {
            "spectral_centroid": spectral_centroid,
            "spectral_bandwidth": spectral_bandwidth,
            "spectral_rolloff": spectral_rolloff,
            "spectral_flatness": spectral_flatness,
            "spectral_crest": spectral_crest,
            "max_amp": float(np.max(amp_arr)),
            "mean_amp": float(np.mean(amp_arr)),
            "std_amp": float(np.std(amp_arr)),
        }

    def extract_optical_features(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> Dict[str, float]:
        alpha_arr = np.array(absorption_coeff, dtype=np.float64)
        n_arr = np.array(refractive_index, dtype=np.float64)

        if len(alpha_arr) < 5:
            return {
                "alpha_mean": 0.0,
                "alpha_std": 0.0,
                "alpha_max": 0.0,
                "alpha_min": 0.0,
                "n_mean": 0.0,
                "n_std": 0.0,
                "n_range": 0.0,
                "alpha_smoothness": 0.0,
            }

        alpha_grad = np.gradient(alpha_arr)
        alpha_smoothness = float(1.0 / (1.0 + np.std(alpha_grad)))

        return {
            "alpha_mean": float(np.mean(alpha_arr)),
            "alpha_std": float(np.std(alpha_arr)),
            "alpha_max": float(np.max(alpha_arr)),
            "alpha_min": float(np.min(alpha_arr)),
            "n_mean": float(np.mean(n_arr)),
            "n_std": float(np.std(n_arr)),
            "n_range": float(np.max(n_arr) - np.min(n_arr)),
            "alpha_smoothness": alpha_smoothness,
        }

    def fit_history(self, feature_dict: Dict[str, float], is_valid: bool = True) -> None:
        if is_valid:
            for key in self._history_buffers:
                if key in feature_dict:
                    self._history_buffers[key].append(feature_dict[key])

        if all(len(buf) > 20 for buf in self._history_buffers.values()):
            self._fitted = True

    def detect_anomaly(
        self,
        time: List[float],
        field: List[float],
        frequencies: Optional[List[float]] = None,
        amplitude: Optional[List[float]] = None,
        alpha: Optional[List[float]] = None,
        n: Optional[List[float]] = None,
        sample_thickness_mm: float = 2.0,
    ) -> Dict:
        time_features = self.extract_time_domain_features(time, field)

        all_features = {**time_features}

        if frequencies is not None and amplitude is not None:
            freq_features = self.extract_freq_domain_features(frequencies, amplitude)
            all_features.update(freq_features)

        if alpha is not None and n is not None and frequencies is not None:
            optical_features = self.extract_optical_features(frequencies, alpha, n)
            all_features.update(optical_features)

        anomaly_scores = self._compute_anomaly_scores(all_features)

        is_invalid, reasons = self._rule_based_check(
            time_features,
            all_features,
            sample_thickness_mm,
        )

        overall_score = float(np.mean(list(anomaly_scores.values())))

        if self._sklearn_available and self._fitted and self._isolation_forest is not None:
            try:
                feature_vector = self._build_feature_vector(all_features)
                sklearn_pred = self._isolation_forest.predict([feature_vector])[0]
                sklearn_score = self._isolation_forest.decision_function([feature_vector])[0]

                is_invalid = is_invalid or sklearn_pred == -1
                overall_score = (overall_score + (1 - sklearn_score)) / 2
            except Exception:
                pass

        if is_invalid and not reasons:
            reasons.append("Statistical anomaly detected")

        is_bubble = any(
            "bubble" in r.lower() or "气泡" in r for r in reasons
        )
        is_thickness_issue = any(
            "thickness" in r.lower() or "厚度" in r.lower() for r in reasons
        )

        return {
            "is_invalid": is_invalid,
            "anomaly_score": overall_score,
            "confidence": float(1.0 - min(1.0, overall_score)),
            "reasons": reasons,
            "features": all_features,
            "anomaly_type": {
                "bubble": is_bubble,
                "thickness_uneven": is_thickness_issue,
                "low_snr": any("snr" in r.lower() for r in reasons),
                "distorted": any("distort" in r.lower() for r in reasons),
            },
            "severity": "high" if overall_score > 0.7 else ("medium" if overall_score > 0.4 else "low"),
        }

    def _compute_anomaly_scores(self, features: Dict[str, float]) -> Dict[str, float]:
        scores = {}

        for key, value in features.items():
            if key in self._history_buffers and len(self._history_buffers[key]) > 10:
                buf = np.array(self._history_buffers[key])
                mean_val = np.mean(buf)
                std_val = np.std(buf) + 1e-10
                z_score = abs((value - mean_val) / std_val)
                scores[key] = float(1.0 - 1.0 / (1.0 + z_score))
            else:
                scores[key] = 0.5

        return scores

    def _rule_based_check(
        self,
        time_features: Dict[str, float],
        all_features: Dict[str, float],
        sample_thickness_mm: float,
    ) -> Tuple[bool, List[str]]:
        reasons = []
        is_invalid = False

        if time_features.get("snr_estimate", 100) < 5.0:
            is_invalid = True
            reasons.append("信号信噪比过低 (< 5dB)")

        if time_features.get("peak_amplitude", 1) == 0:
            is_invalid = True
            reasons.append("信号峰值为零，可能无有效信号")

        if time_features.get("num_peaks", 0) > 20:
            is_invalid = True
            reasons.append("信号中存在过多振荡峰，可能由气泡散射导致 (bubble detection)")

        pulse_width = time_features.get("pulse_width", 0)
        expected_width = sample_thickness_mm * 1e-12 * 10
        if pulse_width > expected_width * 3:
            is_invalid = True
            reasons.append("脉冲宽度异常增大，可能由样品厚度不均导致 (thickness uneven)")

        rise_time = time_features.get("rise_time", 0)
        fall_time = time_features.get("fall_time", 0)
        if rise_time > 0 and fall_time > 0:
            ratio = max(rise_time, fall_time) / min(rise_time, fall_time)
            if ratio > 5:
                is_invalid = True
                reasons.append("上升沿/下降沿严重不对称，信号畸变 (distorted waveform)")

        skewness = time_features.get("skewness", 0)
        kurtosis = time_features.get("kurtosis", 0)
        if abs(skewness) > 2.0:
            is_invalid = True
            reasons.append("信号严重偏态，可能存在系统偏差")
        if abs(kurtosis) > 10:
            is_invalid = True
            reasons.append("信号峰度异常，可能由气泡或杂质引起")

        if "spectral_flatness" in all_features:
            flatness = all_features["spectral_flatness"]
            if flatness > 0.8:
                is_invalid = True
                reasons.append("频谱过于平坦，可能为噪声主导的无效信号")

        if "alpha_smoothness" in all_features:
            smoothness = all_features["alpha_smoothness"]
            if smoothness < 0.3:
                is_invalid = True
                reasons.append("吸收系数曲线过于粗糙，可能存在测量误差")

        return is_invalid, reasons

    def _build_feature_vector(self, features: Dict[str, float]) -> np.ndarray:
        feature_keys = [
            "peak_amplitude",
            "peak_position",
            "pulse_width",
            "rise_time",
            "fall_time",
            "rms",
            "std",
            "skewness",
            "kurtosis",
            "snr_estimate",
            "num_peaks",
        ]

        vector = []
        for key in feature_keys:
            vector.append(features.get(key, 0.0))

        return np.array(vector, dtype=np.float32)

    def update_reference(self, features: Dict[str, float]) -> None:
        self.fit_history(features, is_valid=True)
