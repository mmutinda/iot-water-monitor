// Tank A: TOP
#include <WiFi.h>
#include <PubSubClient.h>
#include "secrets.h"  // Wi-Fi credentials — copy secrets.example.h to secrets.h

#define DEBUG_MODE 0  // 1 = debug ON, 0 = debug OFF

#if DEBUG_MODE
  #define DEBUG_PRINT(x)   Serial.print(x)
  #define DEBUG_PRINTLN(x) Serial.println(x)
#else
  #define DEBUG_PRINT(x)
  #define DEBUG_PRINTLN(x)
#endif

// WiFi credentials
const char* ssid     = SECRET_WIFI_SSID;
const char* password = SECRET_WIFI_PASS;

// MQTT broker (change to your Mosquitto server IP)
const char* mqtt_server    = SECRET_MQTT_HOST;
const int   mqtt_port      = 1883;
const char* mqtt_topic     = "tank_a/level";
const char* mqtt_client_id = "esp32_tank_a";
// Broker login — must match the "sensor" user in the broker password file.
const char* mqtt_user      = SECRET_MQTT_USER;
const char* mqtt_pass      = SECRET_MQTT_PASS;

// HC-SR04 pins
const int TRIG_PIN = 9;
const int ECHO_PIN = 10;

// const unsigned long PUB_INTERVAL = 4000;// 4000;// 300000; // Publish every 5 minutes
const unsigned long PUB_INTERVAL = 60000; //Publish every 1 minutes

WiFiClient   espClient;
PubSubClient mqttClient(espClient);

unsigned long lastPublishTime = 0;


void connectWiFi() {
  DEBUG_PRINT("Connecting to WiFi...");
  if (WiFi.begin(ssid, password)) {
    while (WiFi.status() != WL_CONNECTED) {
      delay(500);
      DEBUG_PRINT(".");
    }
    DEBUG_PRINTLN("\nConnected!");
    DEBUG_PRINT("IP address: ");
    DEBUG_PRINTLN(WiFi.localIP());
  } else {
    DEBUG_PRINTLN("Failed to start WiFi");
  }
}

// ---------------------------------------------------------------------------
// MQTT
// ---------------------------------------------------------------------------
void connectMQTT() {
  while (!mqttClient.connected()) {
    DEBUG_PRINT("Connecting to MQTT broker...");
    if (mqttClient.connect(mqtt_client_id, mqtt_user, mqtt_pass)) {
      DEBUG_PRINTLN("connected!");
    } else {
      DEBUG_PRINT("failed, rc=");
      DEBUG_PRINT(mqttClient.state());
      DEBUG_PRINTLN("  retrying in 5s");
      delay(5000);
    }
  }
}

// ---------------------------------------------------------------------------
// HC-SR04 distance reading
// Returns distance in cm, or -1.0 on timeout (no echo / out of range).
// ---------------------------------------------------------------------------

float readDistanceCM() {
  digitalWrite(TRIG_PIN, LOW);
  delayMicroseconds(2);
  digitalWrite(TRIG_PIN, HIGH);
  delayMicroseconds(10);
  digitalWrite(TRIG_PIN, LOW);

  // 30 000 µs timeout ≈ 5 m max range
  long duration = pulseIn(ECHO_PIN, HIGH, 30000);

  DEBUG_PRINT("Raw duration (µs): ");
  DEBUG_PRINTLN(duration);

  if (duration == 0) {
    return -1.0;  // Sensor timeout
  }

  return (duration * 0.034f) / 2.0f;
}

// ---------------------------------------------------------------------------
void setup() {
  Serial.begin(115200);
  delay(1000);

  pinMode(TRIG_PIN, OUTPUT);
  pinMode(ECHO_PIN, INPUT);

  connectWiFi();

  mqttClient.setServer(mqtt_server, mqtt_port);
}

void loop() {
  // Keep WiFi alive
  if (WiFi.status() != WL_CONNECTED) {
    DEBUG_PRINTLN("WiFi lost — reconnecting...");
    connectWiFi();
  }

  // Keep MQTT alive
  if (!mqttClient.connected()) {
    connectMQTT();
  }
  mqttClient.loop();

  // Publish on interval
  unsigned long now = millis();
  if (now - lastPublishTime >= PUB_INTERVAL) {
    lastPublishTime = now;

    float distance = readDistanceCM();

    if (distance < 0) {
      DEBUG_PRINTLN("Sensor timeout — skipping publish");
      return;
    }

    char payload[64];
    snprintf(payload, sizeof(payload), "{\"distance_cm\":%.1f}", distance);

    DEBUG_PRINT("Publishing to ");
    DEBUG_PRINT(mqtt_topic);
    DEBUG_PRINT(": ");
    DEBUG_PRINTLN(payload);

    mqttClient.publish(mqtt_topic, payload);
  }
}
