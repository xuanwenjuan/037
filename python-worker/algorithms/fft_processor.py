import numpy as np
from typing import Dict, List, Optional


class FFTProcessor:
    def __init__(self):
        pass

    def compute_fft(self, time: np.ndarray, field: np.ndarray) -> Dict:
        n = len(field)
        dt = np.mean(np.diff(time))

        fft_vals = np.fft.fft(field)
        freqs = np.fft.fftfreq(n, d=dt)

        positive_mask = freqs >= 0
        freqs_pos = freqs[positive_mask]
        fft_pos = fft_vals[positive_mask]

        amplitude = np.abs(fft_pos)
        phase = np.unwrap(np.angle(fft_pos))

        return {
            "frequencies": freqs_pos.tolist(),
            "amplitude": amplitude.tolist(),
            "phase": phase.tolist(),
        }

    def process_waveform(
        self,
        time: List[float],
        sample_field: List[float],
        reference_field: Optional[List[float]] = None,
    ) -> Dict:
        time_arr = np.array(time, dtype=np.float64)
        sample_arr = np.array(sample_field, dtype=np.float64)

        mean_subtracted = sample_arr - np.mean(sample_arr)
        windowed = self._apply_hann_window(mean_subtracted)
        sample_result = self.compute_fft(time_arr, windowed)

        result = {
            "frequencies": sample_result["frequencies"],
            "sample_amplitude": sample_result["amplitude"],
            "sample_phase": sample_result["phase"],
        }

        if reference_field is not None and len(reference_field) > 0:
            ref_arr = np.array(reference_field, dtype=np.float64)
            ref_mean = ref_arr - np.mean(ref_arr)
            ref_windowed = self._apply_hann_window(ref_mean)
            ref_result = self.compute_fft(time_arr, ref_windowed)
            result["reference_amplitude"] = ref_result["amplitude"]
            result["reference_phase"] = ref_result["phase"]

        return result

    def _apply_hann_window(self, signal: np.ndarray) -> np.ndarray:
        n = len(signal)
        window = np.hanning(n)
        return signal * window

    def to_terahertz(self, frequencies: List[float]) -> List[float]:
        return [f / 1e12 for f in frequencies]
