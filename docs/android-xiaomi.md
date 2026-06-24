# Android And Xiaomi Roadmap

`mi` should become a local ARM inference network, not a Mac-only tool. Apple Silicon is the first stable provider target, but Android phones and tablets can become useful edge nodes when the node-agent can run natively and advertise hardware capabilities.

## Product Role

Android nodes should be treated differently from always-on Macs:

- `mobile-burst`: opportunistic public or community jobs while charging on Wi-Fi.
- `edge-private-owned`: the user's own phone serving that user's private local tasks.
- `city-opportunistic`: many local devices contributing small amounts of non-sensitive compute.

Avoid strong SLAs or sensitive third-party private prompts on untrusted phones until there is stronger isolation, signed agent distribution, and better device attestation.

## Supported Classes

### Xiaomi Snapdragon

Recent Xiaomi flagships such as Xiaomi 15 and Xiaomi 15 Ultra use Snapdragon 8 Elite with Qualcomm AI Engine. These are the best Xiaomi Android candidates for serious local inference because Qualcomm exposes a mature acceleration stack.

Recommended backend order:

1. `ort-qnn`: ONNX Runtime QNN Execution Provider over Qualcomm AI Engine Direct for Hexagon NPU, Adreno GPU, or CPU fallback.
2. `litert-qnn`: Android-first LiteRT path when Qualcomm NPU delegates are available for the target model.
3. `executorch-qnn`: future PyTorch-edge path for QNN.
4. `llamacpp-vulkan`: portable fallback on Adreno GPU.
5. `cpu-arm64`: last-resort fallback for small models and diagnostics.

### Xiaomi XRing

Xiaomi's XRing chips are strategically important, but they should be treated as a separate target from Snapdragon. Until Xiaomi exposes a public, stable NPU SDK/delegate suitable for third-party LLM serving, the practical path is:

1. `litert-npu` if Xiaomi exposes compatible NPU support.
2. `litert-gpu` for Android-native GPU acceleration.
3. `llamacpp-vulkan` on Arm Mali/Immortalis GPUs.
4. `cpu-arm64` fallback.

Do not assume Qualcomm QNN support on XRing. QNN is for Qualcomm Snapdragon hardware.

## Runtime Strategy

The node-agent now has a backend abstraction. Ollama is the first backend, but the config can describe future Android runtimes:

```yaml
backend:
  type: "ort-qnn"
  url: ""
hardware:
  kind: "android"
  vendor: "xiaomi"
  model: "xiaomi_15_ultra"
  soc: "snapdragon_8_elite"
  accelerators: ["hexagon_npu", "adreno", "cpu"]
  power_mode: "charging_only"
  network_mode: "wifi_only"
```

The coordinator stores and exposes this metadata through node snapshots, network status, and reputation reports. Requests can already ask for `mi_backend`, `mi_device_kind`, `mi_soc`, and `mi_accelerators`, so early Android/Snapdragon experiments can be routed explicitly without changing the public OpenAI-compatible endpoint. Node WebSocket messages carry `version` and `protocol_version` fields so Android, Snapdragon, Apple Silicon, CUDA, Linux, and Windows agents can evolve without forcing every node to update at once.

Example request for a future Xiaomi Snapdragon QNN node:

```json
{
  "model": "fast",
  "mi_backend": "ort-qnn",
  "mi_device_kind": "android",
  "mi_soc": "snapdragon_8_elite",
  "mi_accelerators": ["hexagon_npu"],
  "messages": [{"role": "user", "content": "Run this on the phone NPU"}]
}
```

Future scheduling can also route by power mode, thermal state, price, and challenge score.

## Android Agent Plan

The future Android node should be a native app, not a desktop Go binary running under a terminal:

1. Kotlin foreground service that keeps an outbound WebSocket to the coordinator.
2. Work only when policy allows: charging, Wi-Fi, thermal state normal, battery above threshold.
3. Local backend bridge implemented through JNI or native libraries.
4. Secure enrollment using provider token, optional device attestation, and signed app distribution.
5. No prompt logging; only request IDs, model IDs, timing, backend, and accelerator metadata.

## Model Packaging

Model artifacts will differ by backend:

- Ollama and llama.cpp usually use GGUF-like packaging.
- ONNX Runtime QNN requires ONNX assets prepared for QNN, often quantized and compiled with Qualcomm tooling.
- LiteRT requires LiteRT/TFLite assets and delegates.
- MediaPipe LLM uses its own task packaging.

`mi` aliases should map a user-facing model ID to platform-specific concrete IDs. Future platform-aware aliases can map `fast` to `llama3.1:8b` on Mac/Ollama, `llama3.2-3b-qnn` on Snapdragon/QNN, and a LiteRT model on XRing.

## Challenge Jobs

Benchmark challenges should be platform-specific:

- `android-ort-qnn`
- `android-litert-gpu`
- `android-llamacpp-vulkan`
- `mac-ollama-metal`
- `mac-mlx`

Provider reputation should eventually break out pass rate, latency, and score by backend. This prevents a provider from passing easy CPU checks while advertising accelerated capacity.

## References

- [Xiaomi 15 official specs](https://www.mi.com/global/product/xiaomi-15/specs/): Snapdragon 8 Elite and Qualcomm AI Engine.
- [Xiaomi 15 Ultra official specs](https://www.mi.com/global/product/xiaomi-15-ultra/specs/): Snapdragon 8 Elite and Qualcomm AI Engine.
- [Android NNAPI migration guide](https://developer.android.com/ndk/guides/neuralnetworks/migration-guide): NNAPI is deprecated in Android 15.
- [Google LiteRT](https://developers.google.com/edge/litert): current Android on-device ML runtime direction.
- [LiteRT NPU delegate](https://developers.google.com/edge/litert/android/npu/overview): Android NPU acceleration path.
- [ONNX Runtime QNN Execution Provider](https://onnxruntime.ai/docs/execution-providers/QNN-ExecutionProvider.html): Qualcomm acceleration on Android and Windows Snapdragon.
- [Qualcomm AI Hub](https://aihub.qualcomm.com/): model optimization and profiling for Qualcomm devices.
- [Qualcomm AI Engine Direct SDK](https://www.qualcomm.com/developer/software/qualcomm-ai-engine-direct-sdk): lower-level access to Qualcomm CPU, Adreno GPU, and Hexagon NPU.
