import os
import numpy as np
from sklearn.cross_decomposition import PLSRegression
from sklearn.preprocessing import StandardScaler
from sklearn.model_selection import train_test_split
from sklearn.metrics import mean_squared_error, r2_score
import onnx
from skl2onnx import convert_sklearn
from skl2onnx.common.data_types import FloatTensorType
import pickle


def generate_synthetic_data(n_samples: int = 500, n_freq_points: int = 30):
    np.random.seed(42)

    target_freqs = np.array([0.2, 0.4, 0.6, 0.8, 1.0, 1.2, 1.5, 2.0, 2.5, 3.0]) * 1e12

    n_features = 30

    moisture_contents = np.random.uniform(0.5, 30.0, n_samples)

    X = np.zeros((n_samples, n_features))

    alpha_base = np.array([
        0.5, 0.8, 1.2, 1.8, 2.5, 3.0, 3.2, 2.8, 2.2, 1.8,
        1.6, 1.5, 1.4, 1.3, 1.2, 1.1, 1.0, 0.95, 0.9, 0.85,
        1.52, 1.50, 1.48, 1.46, 1.44, 1.42, 1.40, 1.38, 1.36, 1.34
    ])

    n_base = np.array([
        1.5, 1.52, 1.55, 1.58, 1.6, 1.62, 1.63, 1.62, 1.6, 1.58,
        1.56, 1.54, 1.52, 1.50, 1.48, 1.46, 1.44, 1.42, 1.40, 1.38,
        0.1, 0.08, 0.06, 0.05, 0.04, 0.03, 0.025, 0.02, 0.018, 0.015
    ])

    for i in range(n_samples):
        m = moisture_contents[i]

        moisture_water_abs = 0.3 * m * np.exp(-(target_freqs - 1.5e12) ** 2 / (2 * (0.5e12) ** 2))

        alpha_sample = alpha_base[:10] + moisture_water_abs + np.random.normal(0, 0.1, 10)
        n_sample = n_base[:10] + 0.005 * m + np.random.normal(0, 0.005, 10)
        k_sample = alpha_sample * 3e8 / (4 * np.pi * target_freqs)

        alpha_stats = np.array([
            np.mean(alpha_sample),
            np.std(alpha_sample),
            np.max(alpha_sample),
            np.min(alpha_sample),
            np.median(alpha_sample)
        ])

        n_stats = np.array([
            np.mean(n_sample),
            np.std(n_sample),
            np.max(n_sample),
            np.min(n_sample),
            np.median(n_sample)
        ])

        X[i] = np.concatenate([alpha_sample, n_sample, alpha_stats, n_stats])

    y = moisture_contents

    return X, y, target_freqs


def train_plsr_model(X, y, n_components: int = 10):
    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=0.2, random_state=42
    )

    scaler_X = StandardScaler()
    scaler_y = StandardScaler()

    X_train_scaled = scaler_X.fit_transform(X_train)
    y_train_scaled = scaler_y.fit_transform(y_train.reshape(-1, 1)).flatten()
    X_test_scaled = scaler_X.transform(X_test)

    pls = PLSRegression(n_components=n_components)
    pls.fit(X_train_scaled, y_train_scaled)

    y_pred_scaled = pls.predict(X_test_scaled)
    y_pred = scaler_y.inverse_transform(y_pred_scaled).flatten()

    mse = mean_squared_error(y_test, y_pred)
    r2 = r2_score(y_test, y_pred)
    rmse = np.sqrt(mse)

    print(f"PLSR Model Performance:")
    print(f"  n_components: {n_components}")
    print(f"  R2 Score: {r2:.4f}")
    print(f"  MSE: {mse:.4f}")
    print(f"  RMSE: {rmse:.4f}%")

    return pls, scaler_X, scaler_y, (X_test, y_test, y_pred)


def save_model_to_onnx(pls, scaler_X, scaler_y, output_path: str, n_features: int):
    from sklearn.pipeline import Pipeline

    pipeline = Pipeline([
        ("scaler", scaler_X),
        ("pls", pls),
    ])

    class InverseTransformWrapper:
        def __init__(self, pipeline, scaler_y):
            self.pipeline = pipeline
            self.scaler_y = scaler_y

        def predict(self, X):
            y_scaled = self.pipeline.predict(X)
            return self.scaler_y.inverse_transform(y_scaled.reshape(-1, 1))

    wrapper = InverseTransformWrapper(pipeline, scaler_y)

    initial_type = [("float_input", FloatTensorType([None, n_features]))]

    try:
        onnx_model = convert_sklearn(
            wrapper,
            initial_types=initial_type,
            target_opset=12
        )
        os.makedirs(os.path.dirname(output_path), exist_ok=True)
        onnx.save(onnx_model, output_path)
        print(f"Model saved to {output_path}")
        return True
    except Exception as e:
        print(f"ONNX conversion failed: {e}")
        print("Saving as pickle instead...")

        with open(output_path.replace(".onnx", ".pkl"), "wb") as f:
            pickle.dump({
                "pipeline": pipeline,
                "scaler_y": scaler_y,
                "n_features": n_features
            }, f)
        print(f"Model saved to {output_path.replace('.onnx', '.pkl')}")
        return False


def main():
    print("Generating synthetic training data...")
    X, y, target_freqs = generate_synthetic_data(n_samples=1000)
    print(f"Generated {X.shape[0]} samples with {X.shape[1]} features")

    print("\nTraining PLSR model...")
    pls, scaler_X, scaler_y, test_data = train_plsr_model(X, y, n_components=12)

    X_test, y_test, y_pred = test_data
    print("\nSample predictions:")
    for i in range(5):
        print(f"  True: {y_test[i]:.2f}%, Predicted: {y_pred[i]:.2f}%, Error: {abs(y_test[i] - y_pred[i]):.2f}%")

    model_path = "./models/plsr_model.onnx"
    print(f"\nSaving model to {model_path}...")
    success = save_model_to_onnx(pls, scaler_X, scaler_y, model_path, X.shape[1])

    if not success:
        print("\nNote: ONNX conversion requires skl2onnx package.")
        print("Install with: pip install skl2onnx onnx")

    print("\nTraining complete!")


if __name__ == "__main__":
    main()
