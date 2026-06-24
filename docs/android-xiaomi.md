# Android And Xiaomi Roadmap

`mi` is not an Android inference runtime yet. The repo already has the control-plane hooks needed to add Android and Snapdragon nodes later: node hardware metadata, backend metadata, capability hints, privacy tiers, provider accounting, reputation, and settlement.

This document describes the intended path for Android, Snapdragon, and Xiaomi chips.

## Current Repo Support

Implemented today:

- Node metadata fields for `hardware.kind`, `vendor`, `model`, `soc`, `accelerators`, `power_mode`, and `network_mode`.
- Backend metadata through `backend.type`.
- Request routing hints for backend, device kind, SoC, and accelerators.
- Privacy tiers so mobile/public nodes can be limited to non-sensitive workloads.
- Provider reputation and challenge evidence that can later be segmented by backend.
- Protocol version fields reserved for gradual agent upgrades.

Not implemented today:

- Native Android node app.
- QNN backend.
- LiteRT backend.
- ExecuTorch backend.
- Vulkan llama.cpp backend.
- Mobile power, thermal, or charging policy enforcement.
- Android attestation.
- App signing and distribution flow.

## Product Role For Android Nodes

Android devices should not be treated like always-on Macs.

Useful roles:

- `mobile-burst`: opportunistic public jobs while charging, on Wi-Fi, and thermally healthy.
- `edge-private-owned`: the user's own phone serving the user's own private local tasks.
- `city-opportunistic`: many known local devices contributing small amounts of non-sensitive compute.
- `field-edge`: local inference near cameras, sensors, or offline workflows.

Avoid sensitive third-party private prompts on untrusted phones until there is stronger isolation, signed agent distribution, device attestation, and clearer user consent.

## Xiaomi And Snapdragon Targets

As of June 2026, official Xiaomi global pages list:

- Xiaomi 17 and Xiaomi 17 Ultra with Snapdragon 8 Elite Gen 5.
- Xiaomi 15 and Xiaomi 15 Ultra with Snapdragon 8 Elite.
- Xiaomi XRING O1 as Xiaomi's self-developed 3 nm Arm-based SoC, used in devices such as Xiaomi Pad 7S Pro and earlier XRing launches.

Practical implication:

- Snapdragon devices should use Qualcomm-specific acceleration paths first.
- XRing devices should be treated as a separate Arm/Mali/Android target.
- Do not assume Qualcomm QNN works on XRing hardware.

## Recommended Backend Order: Snapdragon

For Snapdragon 8 Elite and Snapdragon 8 Elite Gen 5 devices:

1. `ort-qnn`: ONNX Runtime with QNN Execution Provider for Qualcomm acceleration.
2. `litert-qnn`: LiteRT path when a Qualcomm delegate is available for the model.
3. `executorch-qnn`: PyTorch-edge path if QNN support is mature for the target model.
4. `llamacpp-vulkan`: portable Adreno GPU fallback for GGUF-style models.
5. `cpu-arm64`: last-resort fallback for small models and diagnostics.

Advertised node profile:

```yaml
backend:
  type: "ort-qnn"
hardware:
  kind: "android"
  vendor: "xiaomi"
  model: "xiaomi_17_ultra"
  soc: "snapdragon_8_elite_gen_5"
  accelerators: ["hexagon_npu", "adreno", "cpu"]
  power_mode: "charging_only"
  network_mode: "wifi_only"
```

Example request:

```json
{
  "model": "fast",
  "mi_backend": "ort-qnn",
  "mi_device_kind": "android",
  "mi_soc": "snapdragon_8_elite_gen_5",
  "mi_accelerators": ["hexagon_npu"],
  "messages": [{"role": "user", "content": "Run this on a Snapdragon NPU node"}]
}
```

## Recommended Backend Order: Xiaomi XRing

For XRing devices:

1. `litert-npu`: only if Xiaomi exposes a stable Android NPU delegate suitable for third-party apps.
2. `litert-gpu`: Android GPU delegate path where supported.
3. `llamacpp-vulkan`: Vulkan path on Arm Mali/Immortalis GPUs.
4. `cpu-arm64`: fallback for small models and health checks.

Advertised node profile:

```yaml
backend:
  type: "llamacpp-vulkan"
hardware:
  kind: "android"
  vendor: "xiaomi"
  model: "xiaomi_pad_7s_pro"
  soc: "xring_o1"
  accelerators: ["mali_immortalis", "cpu"]
  power_mode: "charging_only"
  network_mode: "wifi_only"
```

QNN should not be listed unless the device is actually Qualcomm Snapdragon hardware.

## Android Agent Design

The Android node should be a native app, not a terminal Go binary.

Recommended architecture:

1. Kotlin foreground service maintains an outbound WebSocket to the coordinator.
2. Work policy only allows jobs when charging, on Wi-Fi, battery is above threshold, and thermal state is acceptable.
3. Native backend bridge uses JNI, AAR, or packaged runtime libraries.
4. The app exposes no inbound network port.
5. Enrollment uses provider token, QR link, or one-time join code.
6. Optional Android device attestation is recorded during enrollment.
7. Logs contain request IDs, model IDs, timings, backend, and accelerator metadata, not prompt bodies.
8. The app reports thermal, battery, charging, and network state in heartbeats.

## Scheduler Policy For Mobile Nodes

Mobile nodes should get stricter policy than desktops:

- Default to `public` unless owned by the same user.
- Prefer charging and Wi-Fi state.
- Avoid long generations.
- Apply lower concurrency.
- Use smaller models.
- Penalize thermal throttling.
- Track challenge scores by backend and SoC.
- Avoid strong latency SLAs unless the device is pinned, powered, and cooled.

Future scheduler fields may include:

- Battery percentage.
- Charging state.
- Thermal state.
- Metered network state.
- Estimated energy cost.
- User availability window.

## Model Packaging

Different backends need different model artifacts:

- Ollama usually uses local Ollama model names.
- llama.cpp/Vulkan usually uses GGUF-style artifacts.
- ONNX Runtime QNN needs ONNX models prepared and often quantized for QNN.
- LiteRT needs LiteRT/TFLite artifacts and compatible delegates.
- ExecuTorch needs exported PyTorch-edge artifacts.

`mi` model aliases should stay stable while mapping to platform-specific targets:

```yaml
models:
  aliases:
    - id: "fast"
      target: "llama3.1:8b"
```

Future platform-aware aliasing could map:

- `fast` on Mac/Ollama -> `llama3.1:8b`
- `fast` on Mac/MLX -> `llama-3.1-8b-mlx`
- `fast` on Snapdragon/QNN -> `llama-3.2-3b-qnn`
- `fast` on XRing/Vulkan -> `small-gguf-vulkan`

## Challenge Jobs

Challenge labels should include platform and backend:

- `mac-ollama-metal`
- `mac-mlx`
- `android-ort-qnn`
- `android-litert-gpu`
- `android-llamacpp-vulkan`
- `linux-cuda-vllm`
- `windows-snapdragon-qnn`

Provider reputation should eventually separate pass rate, latency, throughput, and failure rate by backend and hardware class. A provider that passes CPU checks should not automatically be trusted for accelerated NPU capacity.

## Implementation Steps

1. Add a minimal Android app shell with foreground service and outbound WebSocket.
2. Reuse the existing node registration and heartbeat protocol.
3. Report Android power, network, and thermal metadata.
4. Add a CPU-only diagnostic backend first.
5. Add llama.cpp/Vulkan for portable GPU experiments.
6. Add ONNX Runtime QNN for Snapdragon devices.
7. Add LiteRT experiments for Android-native delegates.
8. Add provider enrollment links or QR codes.
9. Add Android-specific challenge jobs.
10. Add signed app distribution and optional attestation before untrusted rental.

## References

- [Xiaomi 17 specs](https://www.mi.com/global/product/xiaomi-17/)
- [Xiaomi 17 Ultra specs](https://www.mi.com/global/product/xiaomi-17-ultra/)
- [Xiaomi 15 specs](https://www.mi.com/global/product/xiaomi-15/specs/)
- [Xiaomi 15 Ultra specs](https://www.mi.com/global/product/xiaomi-15-ultra/specs/)
- [Xiaomi XRING O1 overview](https://www.mi.com/global/discover/article?id=4926)
- [Android NNAPI migration guide](https://developer.android.com/ndk/guides/neuralnetworks/migration-guide)
- [Google LiteRT](https://developers.google.com/edge/litert)
- [LiteRT NPU delegate](https://developers.google.com/edge/litert/android/npu/overview)
- [ONNX Runtime QNN Execution Provider](https://onnxruntime.ai/docs/execution-providers/QNN-ExecutionProvider.html)
- [Qualcomm AI Hub](https://aihub.qualcomm.com/)
- [Qualcomm AI Engine Direct SDK](https://www.qualcomm.com/developer/software/qualcomm-ai-engine-direct-sdk)
