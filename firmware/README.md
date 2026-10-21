# Ultrasonic Sensor Firmware

Sensor sketches publish distance readings over MQTT for the Go backend:

```json
{"distance_cm":75.0}
```

| Sketch folder | Sensor | Pins | MQTT topic | Publish interval |
|---|---|---|---|---|
| `esp32_ground_tank` | HY-SRF05 pulse/echo | TRIG GPIO4, ECHO GPIO5 | `tank_a/level` | 60 seconds |
| `sketch_tankBwithWaterProofSensor` | A02YYUW waterproof UART | Sensor TX to GPIO26 (RX), sensor RX to GPIO17 (TX, optional) | `tank_b/level` | 3 seconds |
| `sketch_arduino_R4_tankA` | Legacy HC-SR04 pulse/echo | TRIG 9, ECHO 10 | `tank_a/level` | 60 seconds |

The ground-tank and waterproof sketches target ESP32. The legacy folder name
mentions Arduino R4, but its source uses `WiFi.h` and an ESP32-style MQTT client
identifier; do not assume it supports UNO R4 unchanged.

## Configure locally

1. Open the desired `.ino` file in Arduino IDE with the appropriate board
   support installed. The ESP32 sketches need the Espressif ESP32 board package.
2. Install the **PubSubClient** library.
3. Copy that sketch's `secrets.example.h` to `secrets.h` in the same folder.
4. Set your own Wi-Fi SSID/password, MQTT broker host, and MQTT username/password
   in `secrets.h`. Use the broker's device-reachable LAN address, not localhost.
5. Verify the pins and sensor voltage levels for your board before uploading.

Use a voltage divider or level shifter for a 5 V echo signal connected to an
ESP32 input. The HY-SRF05 wiring notes are at the top of its sketch.

Only placeholder templates are committed. `secrets.h` and firmware build
outputs are ignored by Git. No production broker address is included.

These sketches use unencrypted MQTT on port 1883. Use an isolated local broker
for a demonstration, with sensor accounts restricted to their reading topics.
Do not run both tank-A sketches simultaneously without configuring distinct
topics/client identifiers.
