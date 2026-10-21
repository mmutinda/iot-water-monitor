// secrets.example.h — template for credentials kept out of source control.
//
// Copy this file to "secrets.h" in the same folder and fill in real values:
//     cp secrets.example.h secrets.h
//
// secrets.h is listed in .gitignore and must never be committed.
#pragma once

#define SECRET_WIFI_SSID  "YOUR_WIFI_SSID"
#define SECRET_WIFI_PASS  "YOUR_WIFI_PASSWORD"

// Use a broker reachable from your device, not localhost on the device.
#define SECRET_MQTT_HOST  "YOUR_MQTT_BROKER_HOST"
#define SECRET_MQTT_USER  "YOUR_MQTT_USERNAME"
#define SECRET_MQTT_PASS  "YOUR_MQTT_PASSWORD"
