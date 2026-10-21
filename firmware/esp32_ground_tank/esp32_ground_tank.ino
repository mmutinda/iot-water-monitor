// Ground tank: publishes readings for tank A
// HY-SRF05 wiring (mode 1):
// VCC -> 5V, GND -> GND, TRIG -> GPIO4, ECHO -> GPIO5 through a 5V-to-3.3V divider
#include <WiFi.h>
#include <PubSubClient.h>
#include "secrets.h"

#define DEBUG_MODE 0

#if DEBUG_MODE
  #define DEBUG_PRINT(x) Serial.print(x)
  #define DEBUG_PRINTLN(x) Serial.println(x)
#else
  #define DEBUG_PRINT(x)
  #define DEBUG_PRINTLN(x)
#endif

const char* ssid = SECRET_WIFI_SSID;
const char* password = SECRET_WIFI_PASS;

const char* mqtt_server = SECRET_MQTT_HOST;
const int mqtt_port = 1883;
const char* mqtt_topic = "tank_a/level";
const char* mqtt_user = SECRET_MQTT_USER;
const char* mqtt_pass = SECRET_MQTT_PASS;
char mqttClientId[32];

constexpr int SENSOR_TRIG_PIN = 4;
constexpr int SENSOR_ECHO_PIN = 5;
constexpr unsigned long SENSOR_ECHO_TIMEOUT_US = 30000;
constexpr float SPEED_OF_SOUND_CM_PER_US = 0.0343F;

const unsigned long PUB_INTERVAL = 60000;  // Publish every minute

WiFiClient espClient;
PubSubClient mqttClient(espClient);

unsigned long lastPublishTime = 0;

void connectWiFi() {
  DEBUG_PRINT("Connecting to WiFi...");
  WiFi.begin(ssid, password);

  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    DEBUG_PRINT(".");
  }

  DEBUG_PRINTLN("\nConnected!");
  DEBUG_PRINT("IP address: ");
  DEBUG_PRINTLN(WiFi.localIP());
}

void connectMQTT() {
  while (!mqttClient.connected()) {
    DEBUG_PRINT("Connecting to MQTT broker...");

    if (mqttClient.connect(
            mqttClientId, mqtt_user, mqtt_pass)) {
      DEBUG_PRINTLN("connected!");
    } else {
      DEBUG_PRINT("failed, rc=");
      DEBUG_PRINT(mqttClient.state());
      DEBUG_PRINTLN(" retrying in 5s");
      delay(5000);
    }
  }
}

float readDistanceCM() {
  digitalWrite(SENSOR_TRIG_PIN, LOW);
  delayMicroseconds(2);
  digitalWrite(SENSOR_TRIG_PIN, HIGH);
  delayMicroseconds(10);
  digitalWrite(SENSOR_TRIG_PIN, LOW);

  const unsigned long echoDurationUs =
      pulseIn(SENSOR_ECHO_PIN, HIGH, SENSOR_ECHO_TIMEOUT_US);

  DEBUG_PRINT("HY-SRF05 echo duration (us): ");
  DEBUG_PRINTLN(echoDurationUs);

  if (echoDurationUs == 0) {
    DEBUG_PRINTLN("HY-SRF05 timeout");
    return -1.0F;
  }

  const float distanceCm =
      echoDurationUs * SPEED_OF_SOUND_CM_PER_US / 2.0F;

  DEBUG_PRINT("Distance (cm): ");
  DEBUG_PRINTLN(distanceCm);

  return distanceCm;
}

void setup() {
  Serial.begin(115200);
  delay(1000);

  pinMode(SENSOR_TRIG_PIN, OUTPUT);
  pinMode(SENSOR_ECHO_PIN, INPUT);
  digitalWrite(SENSOR_TRIG_PIN, LOW);

  const uint32_t deviceId =
      static_cast<uint32_t>(ESP.getEfuseMac() & 0xFFFFFF);
  snprintf(
      mqttClientId, sizeof(mqttClientId),
      "esp32_tank_a_%06lX", static_cast<unsigned long>(deviceId));

  DEBUG_PRINT("MQTT client ID: ");
  DEBUG_PRINTLN(mqttClientId);

  connectWiFi();
  mqttClient.setServer(mqtt_server, mqtt_port);
}

void loop() {
  if (WiFi.status() != WL_CONNECTED) {
    DEBUG_PRINTLN("WiFi lost - reconnecting...");
    connectWiFi();
  }

  if (!mqttClient.connected()) {
    connectMQTT();
  }
  mqttClient.loop();

  const unsigned long now = millis();
  if (now - lastPublishTime >= PUB_INTERVAL) {
    lastPublishTime = now;

    const float distance = readDistanceCM();
    if (distance < 0.0F) {
      DEBUG_PRINTLN("Skipping publish - no valid HY-SRF05 reading");
      return;
    }

    char payload[64];
    snprintf(
        payload, sizeof(payload), "{\"distance_cm\":%.1f}", distance);

    DEBUG_PRINT("Publishing to ");
    DEBUG_PRINT(mqtt_topic);
    DEBUG_PRINT(": ");
    DEBUG_PRINTLN(payload);

    if (!mqttClient.publish(mqtt_topic, payload)) {
      DEBUG_PRINTLN("MQTT publish failed");
    }
  }
}
