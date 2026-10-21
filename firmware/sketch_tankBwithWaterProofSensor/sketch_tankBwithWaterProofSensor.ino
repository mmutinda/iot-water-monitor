// Tank B: Roof top — A02YYUW waterproof ultrasonic sensor
// Wiring: sensor TX → GPIO26 (RX2), sensor RX → GPIO17 (TX2, optional)
// Packet: [0xFF][H][L][SUM]  SUM = (0xFF + H + L) & 0xFF
#include <WiFi.h>
#include <PubSubClient.h>
#include "secrets.h"  // Wi-Fi credentials — copy secrets.example.h to secrets.h

#define DEBUG_MODE 0  // 1 = debug ON, 0 = debug OFF

#if DEBUG_MODE
  #define DEBUG_PRINT(x)    Serial.print(x)
  #define DEBUG_PRINTLN(x)  Serial.println(x)
  #define DEBUG_PRINTF(...) Serial.printf(__VA_ARGS__)
#else
  #define DEBUG_PRINT(x)
  #define DEBUG_PRINTLN(x)
  #define DEBUG_PRINTF(...)
#endif

// WiFi credentials
const char* ssid     = SECRET_WIFI_SSID;
const char* password = SECRET_WIFI_PASS;

// MQTT broker
const char* mqtt_server    = SECRET_MQTT_HOST;
const int   mqtt_port      = 1883;
const char* mqtt_topic     = "tank_b/level";
const char* mqtt_client_id = "esp32_tank_b_new";
// Broker login — must match the "sensor" user in the broker password file.
const char* mqtt_user      = SECRET_MQTT_USER;
const char* mqtt_pass      = SECRET_MQTT_PASS;

// A02YYUW on UART2
HardwareSerial SensorSerial(2);
const int SENSOR_RX_PIN = 26;
const int SENSOR_TX_PIN = 17;

const unsigned long PUB_INTERVAL = 3000; // 180000; // 3 minutes

WiFiClient   espClient;
PubSubClient mqttClient(espClient);

unsigned long lastPublishTime = 0;

// ---------------------------------------------------------------------------
// WiFi
// ---------------------------------------------------------------------------
void connectWiFi() {
  DEBUG_PRINTLN("\nConnecting to WiFi (DHCP)...");
  WiFi.mode(WIFI_STA);
  WiFi.begin(ssid, password);

  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    DEBUG_PRINT(".");
  }

  DEBUG_PRINTLN("\nWiFi connected!");
  DEBUG_PRINT("IP address : "); DEBUG_PRINTLN(WiFi.localIP());
  DEBUG_PRINT("Gateway    : "); DEBUG_PRINTLN(WiFi.gatewayIP());
  DEBUG_PRINT("Subnet     : "); DEBUG_PRINTLN(WiFi.subnetMask());
}

// ---------------------------------------------------------------------------
// MQTT
// ---------------------------------------------------------------------------
void checkTCPReach() {
  DEBUG_PRINTF("Testing TCP reach to %s:%d ...", mqtt_server, mqtt_port);
  WiFiClient testClient;
  if (testClient.connect(mqtt_server, mqtt_port)) {
    DEBUG_PRINTLN(" reachable!");
    testClient.stop();
  } else {
    DEBUG_PRINTLN(" UNREACHABLE — check server IP, port, and firewall");
  }
}

void connectMQTT() {
  checkTCPReach();
  while (!mqttClient.connected()) {
    DEBUG_PRINTF("Connecting to MQTT broker as '%s'...", mqtt_client_id);
    if (mqttClient.connect(mqtt_client_id, mqtt_user, mqtt_pass)) {
      DEBUG_PRINTLN(" connected!");
    } else {
      int rc = mqttClient.state();
      DEBUG_PRINTF(" failed (rc=%d)", rc);
      if (rc == -2)      DEBUG_PRINTLN(" → server unreachable");
      else if (rc == -4) DEBUG_PRINTLN(" → connection timed out");
      else if (rc == 4)  DEBUG_PRINTLN(" → bad credentials");
      else if (rc == 5)  DEBUG_PRINTLN(" → unauthorized");
      else               DEBUG_PRINTLN("");
      delay(5000);
    }
  }
}

// ---------------------------------------------------------------------------
// A02YYUW — read one valid packet, blocking up to timeout_ms
// Returns distance in cm, or -1.0 on timeout / checksum error
// ---------------------------------------------------------------------------
float readDistanceCM(unsigned long timeout_ms = 500) {
  // Discard stale RX bytes so we read a fresh packet
  while (SensorSerial.available()) SensorSerial.read();
  delay(150); // wait for one fresh packet from sensor (~100ms cycle)
  unsigned long start = millis();

  while (millis() - start < timeout_ms) {
    if (!SensorSerial.available()) continue;

    // Sync to header byte
    if (SensorSerial.peek() != 0xFF) {
      SensorSerial.read();
      continue;
    }

    // Wait for full 4-byte packet
    unsigned long t = millis();
    while (SensorSerial.available() < 4) {
      if (millis() - t > 100) break;
    }
    if (SensorSerial.available() < 4) continue;

    uint8_t data[4];
    for (int i = 0; i < 4; i++) data[i] = SensorSerial.read();

    uint8_t checksum = (data[0] + data[1] + data[2]) & 0xFF;
    if (checksum != data[3]) {
      DEBUG_PRINTF("Checksum error: %02X %02X %02X %02X\n",
                   data[0], data[1], data[2], data[3]);
      continue;
    }

    int distance_mm = (data[1] << 8) | data[2];
    float distance_cm = distance_mm / 10.0f;
    DEBUG_PRINTF("Sensor reading: %d mm (%.1f cm)\n", distance_mm, distance_cm);
    return distance_cm;
  }

  DEBUG_PRINTLN("Sensor timeout — no valid packet received");
  return -1.0f;
}

// ---------------------------------------------------------------------------
void setup() {
  Serial.begin(115200);
  delay(2000);

  DEBUG_PRINTLN("=== Tank B starting ===");

  SensorSerial.begin(9600, SERIAL_8N1, SENSOR_RX_PIN, SENSOR_TX_PIN);
  DEBUG_PRINTLN("Sensor initialized");

  connectWiFi();

  mqttClient.setServer(mqtt_server, mqtt_port);
  connectMQTT();
}

void loop() {
  if (WiFi.status() != WL_CONNECTED) {
    DEBUG_PRINTLN("WiFi lost — reconnecting...");
    connectWiFi();
  }

  if (!mqttClient.connected()) {
    connectMQTT();
  }
  mqttClient.loop();

  unsigned long now = millis();
  if (now - lastPublishTime >= PUB_INTERVAL) {
    lastPublishTime = now;

    DEBUG_PRINTLN("Reading sensor...");
    float distance_cm = readDistanceCM();

    if (distance_cm < 0) {
      DEBUG_PRINTLN("Skipping publish — no valid reading");
      return;
    }

    char payload[64];
    snprintf(payload, sizeof(payload), "{\"distance_cm\":%.1f}", distance_cm);

    DEBUG_PRINT("Publishing to ");
    DEBUG_PRINT(mqtt_topic);
    DEBUG_PRINT(": ");
    DEBUG_PRINTLN(payload);

    if (mqttClient.publish(mqtt_topic, payload)) {
      DEBUG_PRINTLN("Publish OK");
    } else {
      DEBUG_PRINTLN("Publish FAILED");
    }
  }
}
