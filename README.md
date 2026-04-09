# Audio Bridge - Smartgroup Notarías

Bridge de audio entre Asterisk y el módulo de IA para el proyecto de Automatización de Notarías.

## Arquitectura

```
Llamada entrante              Audio Bridge                 Módulo IA
┌──────────┐    AudioSocket   ┌──────────────┐    WSS     ┌──────────┐
│          │ ──────TCP──────> │              │ ────WSS───> │          │
│ Asterisk │    PCM 16bit     │    Bridge    │   PCM+JSON  │  STT/LLM │
│          │ <──────TCP────── │    (Go)      │ <───WSS──── │  /TTS    │
└──────────┘                  └──────────────┘             └──────────┘
     │                              │
     │  AMI (transfer/hangup/       │  API REST
     │   originate)                 │  (outbound calls)
     └──────────────────────────────┘
```

## Componentes

| Componente | Puerto | Función |
|-----------|--------|---------|
| AudioSocket Server | 9092 | Recibe audio de Asterisk |
| API REST | 8080 | Llamadas salientes, status, stats |
| AMI Client | → 5038 | Controla Asterisk (transferir, colgar, originar) |
| WSS Client | → IA | Envía/recibe audio y eventos al módulo de IA |

## Requisitos

- Go 1.22+
- Asterisk 18+ con `res_audiosocket` cargado
- Acceso AMI al Asterisk
- Endpoint WSS del módulo de IA

## Instalación

```bash
# Clonar
git clone https://github.com/smartgroup/audio-bridge.git
cd audio-bridge

# Compilar
go build -o audio-bridge ./cmd/bridge

# O con Docker
docker build -t audio-bridge .
```

## Configuración

Editar `configs/config.yaml`:

```yaml
server:
  audiosocket_addr: "0.0.0.0:9092"
  max_concurrent: 50

asterisk:
  ami_host: "192.168.1.10"       # IP de tu Asterisk
  ami_port: 5038
  ami_user: "bridge"
  ami_password: "tu_password"

ai:
  endpoint: "wss://ia.ctnotariado.es/v1/stream"
  api_key: "tu_api_key"

tenants:
  - notaria_id: "N001"
    ddis: ["934001234"]
    # ... ver config.yaml para ejemplo completo
```

## Configuración de Asterisk

### 1. Cargar módulo AudioSocket

```
CLI> module load res_audiosocket.so
```

En `modules.conf`:
```
load => res_audiosocket.so
```

### 2. Configurar AMI para el Bridge

En `manager.conf`:
```ini
[bridge]
secret = tu_password
deny = 0.0.0.0/0.0.0.0
permit = 192.168.1.0/255.255.255.0
read = system,call,log,agent,user
write = system,call,agent,user,originate
```

### 3. Añadir dialplan

Copiar `configs/dialplan-notarias.conf` a tu Asterisk y hacer `#include` desde `extensions.conf`. Ajustar IPs, DDIs y extensiones.

## Ejecución

```bash
# Directo
./audio-bridge -config configs/config.yaml

# Docker
docker run -d \
  -p 9092:9092 \
  -p 8080:8080 \
  -v $(pwd)/configs:/etc/audio-bridge \
  audio-bridge

# Docker Compose (ver docker-compose.yaml)
docker-compose up -d
```

## API REST

### Health Check
```bash
curl http://localhost:8080/health
```

### Originar llamada saliente
```bash
curl -X POST http://localhost:8080/api/v1/calls/outbound \
  -H "X-API-Key: tu_api_key" \
  -H "Content-Type: application/json" \
  -d '{
    "destination": "+34612345678",
    "notaria_id": "N001",
    "call_type": "callback",
    "context_id": "12345abcd",
    "context_data": {
      "expediente": "EXP-2026-0042",
      "motivo": "Confirmación de cita"
    }
  }'
```

### Consultar estado de llamada
```bash
curl http://localhost:8080/api/v1/calls/{call_id}/status \
  -H "X-API-Key: tu_api_key"
```

### Llamadas activas
```bash
curl http://localhost:8080/api/v1/calls/active \
  -H "X-API-Key: tu_api_key"
```

### Estadísticas
```bash
curl http://localhost:8080/api/v1/stats \
  -H "X-API-Key: tu_api_key"
```

## Protocolo WSS con el módulo de IA

### Handshake
```
wss://ia.ctnotariado.es/v1/stream
  ?notaria_id=N001
  &caller_id=34912345678
  &interaction_id=uuid
  &call_type=inbound
  &schedule=business_hours
  &ddi_origin=934001234

Headers:
  X-API-Key: <api_key>
```

### Frames
- **Binarios**: Audio PCM 16-bit signed LE, 16 kHz, mono
- **Texto (JSON)**: Eventos de control

### Eventos IA → Bridge
```json
{"event": "transfer", "destination": "201", "destination_type": "extension", "notaria_id": "N001", "via": "sip_trunk"}
{"event": "hangup", "reason": "resolved"}
{"event": "hold", "action": "start", "moh": true}
```

### Eventos Bridge → IA
```json
{"event": "call_ended", "reason": "caller_hangup", "duration_seconds": 45}
{"event": "dtmf_received", "digit": "1"}
{"event": "transfer_completed", "destination": "201", "status": "connected"}
```

## Estructura del proyecto

```
audio-bridge/
├── cmd/bridge/main.go              # Punto de entrada
├── internal/
│   ├── audiosocket/server.go       # Listener AudioSocket (Asterisk → Bridge)
│   ├── wssclient/client.go         # Cliente WSS (Bridge → IA)
│   ├── bridge/bridge.go            # Lógica central que une ambos lados
│   ├── ami/client.go               # Control de Asterisk vía AMI
│   ├── api/server.go               # API REST (llamadas salientes, status)
│   ├── config/config.go            # Configuración y tenant registry
│   └── models/models.go            # Modelos de datos (Call, Events, etc.)
├── configs/
│   ├── config.yaml                 # Configuración del Bridge
│   └── dialplan-notarias.conf      # Dialplan de ejemplo para Asterisk
├── Dockerfile
├── go.mod
└── README.md
```

## Añadir una nueva notaría

1. Añadir la entrada en `configs/config.yaml` bajo `tenants:`
2. Añadir el DDI en el dialplan (`dialplan-notarias.conf`)
3. Configurar el SIP trunk de retorno en `sip.conf`/`pjsip.conf`
4. Recargar: `asterisk -rx "dialplan reload"` + reiniciar Bridge

## Monitorización

El Bridge genera logs estructurados en JSON (configurable). Cada llamada tiene un `call_id` (correlation ID) que permite trazar end-to-end.

Métricas disponibles en `/api/v1/stats` y `/health`.

Para monitorización avanzada, integrar con Prometheus/Grafana exportando las métricas del API.
