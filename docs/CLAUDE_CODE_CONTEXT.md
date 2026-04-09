# Audio Bridge — Contexto para Claude Code
## Proyecto Automatización Notarías — Smartgroup

---

## Quién soy

Smartgroup es una empresa de tecnología española que da servicios de VoIP, networking y soporte IT. Tenemos experiencia con Asterisk/PekePBX, Yeastar, MikroTik, DrayTek y Zoho CRM. Somos un equipo técnico pequeño (~3 personas).

---

## El proyecto

El Consejo General del Notariado (CTNotariado) está montando un sistema de IA conversacional para automatizar la atención telefónica de las notarías españolas. Hay tres actores:

- **Notaría**: tiene su propia PBX y teléfonos
- **CTNotariado**: responsable de la plataforma central y los servicios de IA (STT, TTS, LLM)
- **Smartgroup (nosotros)**: Proveedor de Integración de Voz — responsables de la capa de señalización SIP y media dentro de la plataforma central (CTN)

### Nuestro rol

Nosotros somos el **puente entre la telefonía y la IA**. Tenemos que:

1. **Recibir las llamadas** de las notarías en nuestra centralita (ya la tenemos, es PekePBX multitenant)
2. **Enviar el audio en streaming** al módulo de IA en tiempo real (bidireccional)
3. **Ejecutar acciones** que la IA nos pide: transferir llamadas, colgar, originar llamadas salientes
4. **Registrar** logs, CDRs y reporting de todo el tráfico

### Lo que NO hacemos

No hacemos nada de IA. No transcribimos, no generamos respuestas, no sintetizamos voz. Eso es de otro proveedor.

---

## Arquitectura

```
Llamada entrante              Audio Bridge                 Módulo IA
┌──────────┐    AudioSocket   ┌──────────────┐    WSS     ┌──────────┐
│          │ ──────TCP──────> │              │ ────WSS───> │          │
│ Asterisk │    PCM 16bit     │    Bridge    │   PCM+JSON  │  STT/LLM │
│ PekePBX  │ <──────TCP────── │    (Go)      │ <───WSS──── │  /TTS    │
└──────────┘                  └──────────────┘             └──────────┘
     │                              │
     │  AMI (transfer/hangup/       │  API REST
     │   originate)                 │  (outbound calls)
     └──────────────────────────────┘
```

### Flujo de llamada entrante

1. Cliente llama a la notaría → PBX de la notaría transfiere a nuestro Asterisk
2. Nuestro dialplan identifica la notaría por DDI, evalúa horario
3. Si cumple condiciones → `AudioSocket(uuid,bridge-host:9092)`
4. Bridge recibe el audio, abre WSS contra el módulo de IA, envía metadatos en handshake
5. Audio fluye bidireccional: Asterisk ↔ AudioSocket ↔ Bridge ↔ WSS ↔ IA
6. Si la IA dice "transfiere" → Bridge ejecuta transfer vía AMI
7. Si la IA dice "cuelga" → Bridge ejecuta hangup vía AMI

### Flujo de llamada saliente (callback)

1. Sistema externo hace POST /api/v1/calls/outbound al Bridge
2. Bridge le dice a Asterisk que origine la llamada vía AMI
3. Asterisk llama, aplica CPD/CPA (detección persona/buzón)
4. Si contesta persona → misma conexión AudioSocket + WSS que en entrante

---

## Stack tecnológico

| Componente | Tecnología | Puerto |
|-----------|-----------|--------|
| Centralita | Asterisk/PekePBX (ya existe, CentOS/Rocky) | SIP |
| Bridge de Audio | **Go** (goroutines para concurrencia) | 9092 (AudioSocket) |
| API REST | Go con Gin | 8080 |
| Control Asterisk | AMI (Asterisk Manager Interface) | → 5038 |
| Conexión con IA | WebSocket Secure (WSS) | → endpoint IA |
| Echo server (test) | Python con websockets | 9093 |

### Por qué Go

Necesitamos manejar muchas conexiones de audio concurrentes en tiempo real (50+ simultáneas en MVP). Go con goroutines es perfecto: cada llamada tiene 2 goroutines (una leyendo de AudioSocket, otra leyendo del WSS), y Go escala sin problemas.

---

## Estructura del proyecto

```
audio-bridge/
├── cmd/bridge/main.go              # Punto de entrada — conecta todo
├── internal/
│   ├── audiosocket/server.go       # Listener AudioSocket (Asterisk → Bridge)
│   ├── wssclient/
│   │   ├── client.go               # Cliente WSS genérico (protocolo propio)
│   │   └── openai_adapter.go       # Adaptador para OpenAI Realtime API
│   ├── bridge/bridge.go            # Lógica central: une AudioSocket con WSS
│   ├── ami/client.go               # Control de Asterisk vía AMI
│   ├── api/server.go               # API REST (outbound calls, status, stats)
│   ├── config/config.go            # Configuración YAML + tenant registry
│   └── models/models.go            # Modelos de datos (Call, Events, registries)
├── configs/
│   ├── config.yaml                 # Configuración de ejemplo
│   └── dialplan-notarias.conf      # Dialplan de ejemplo para Asterisk
├── scripts/
│   └── echo_server.py              # Echo WSS server para pruebas
├── docs/
│   ├── DEPLOY_VPS.md               # Guía de despliegue en VPS
│   └── TEST_LOCAL_PEKEPBX.md       # Guía de pruebas en local con PekePBX
├── Dockerfile
├── go.mod
└── README.md
```

---

## Protocolo WSS con el módulo de IA

### Handshake

```
wss://<endpoint>/v1/stream
  ?notaria_id=N001
  &caller_id=34912345678
  &interaction_id=<uuid>
  &call_type=inbound|callback
  &schedule=business_hours|after_hours
  &ddi_origin=934001234

Headers:
  X-API-Key: <key>  o  Authorization: Bearer <token>
```

### Frames

- **Binarios**: Audio PCM 16-bit signed LE, 16 kHz, mono
- **Texto JSON**: Eventos de control

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

---

## API REST

```
GET  /health                        → health check (no auth)
POST /api/v1/calls/outbound         → originar llamada saliente
GET  /api/v1/calls/{id}/status      → estado de una llamada
GET  /api/v1/calls/active           → llamadas activas
GET  /api/v1/stats                  → estadísticas

Autenticación: X-API-Key header
```

### Ejemplo outbound:

```json
POST /api/v1/calls/outbound
{
  "destination": "+34612345678",
  "notaria_id": "N001",
  "call_type": "callback",
  "context_id": "12345abcd",
  "context_data": {"expediente": "EXP-2026-0042"}
}
```

---

## OpenAI Realtime API (para pruebas)

Tenemos un adaptador (`openai_adapter.go`) que traduce nuestro protocolo al de OpenAI:

- OpenAI usa audio base64 dentro de JSON (no frames binarios puros)
- OpenAI tiene su propio esquema de eventos (session.update, input_audio_buffer.append, response.audio.delta, etc.)
- El adaptador define dos "tools" (funciones) que OpenAI puede llamar: `transfer_call` y `hangup_call`
- Incluye un prompt en español para que la IA actúe como asistente de notaría

Endpoint: `wss://api.openai.com/v1/realtime?model=gpt-realtime-mini`
Audio format: G.711 a-law (nativo de telefonía, evita resampling)
Auth: Bearer token con API key de OpenAI

---

## Estado actual del proyecto

### Lo que está hecho
- ✅ Código completo del Bridge en Go (compilable, no probado en producción)
- ✅ Adaptador OpenAI Realtime
- ✅ Echo server de pruebas en Python
- ✅ Dialplan de ejemplo para Asterisk
- ✅ Configuración multi-tenant YAML
- ✅ API REST con Gin
- ✅ Documentación de despliegue
- ✅ Propuesta técnica enviada a CTNotariado
- ✅ Requisitos de integración enviados al proveedor de IA

### Lo que falta
- ⬜ Instalar Go en la PBX de test (CentOS/Rocky)
- ⬜ Compilar el Bridge
- ⬜ Verificar que AudioSocket existe en el Asterisk de PekePBX
- ⬜ Configurar AMI para el Bridge
- ⬜ Añadir extensión de prueba (899) en el dialplan
- ⬜ Probar con echo server (oírse a uno mismo)
- ⬜ Crear cuenta OpenAI y obtener API key
- ⬜ Probar con OpenAI Realtime (hablar con el bot)
- ⬜ Desplegar en VPS para producción

### Siguiente paso inmediato
**Instalar Go, compilar el Bridge, y probar con el echo server en la PBX de test.**

---

## Entorno de la PBX de test

- **OS**: CentOS/Rocky Linux
- **PBX**: PekePBX (Asterisk por debajo, multitenant)
- **Asterisk version**: 16+ (verificar con `asterisk -rx "core show version"`)
- **Ubicación del proyecto**: `/opt/audio-bridge/`
- **El Bridge correrá en la misma máquina** para las pruebas iniciales

---

## Requisitos del proyecto (RNF relevantes)

- RNF-V01/V02: Recibir llamadas vía PSTN y SIP trunk (TLS/SRTP)
- RNF-V03: Streaming audio bidireccional (1 stream por llamada)
- RNF-V04: Info contextual a la IA (notaria_id, caller_id, horario...)
- RNF-V05/V06: Transferencia de vuelta a la notaría
- RNF-V07: Securización conexión IA (WSS, API key, OAuth2)
- RNF-V09: Reporting (timestamp, llamante, llamado, resultado, duración)
- RNF-V11: Llamadas salientes automáticas con CPD/CPA
- RNF-P05: Latencia ≤ 3s E2E
- RNF-P06: Mínimo 50 llamadas simultáneas (MVP)
- RNF-D01: Disponibilidad 99,9%

---

## Instrucciones para Claude Code

Cuando trabajes en este proyecto:

1. **El código fuente está en `/opt/audio-bridge/`**
2. **Compilar**: `cd /opt/audio-bridge && go build -o audio-bridge ./cmd/bridge`
3. **Configuración**: `configs/config.local.yaml` (NO editar config.yaml, es el ejemplo)
4. **Logs de Asterisk**: `asterisk -rx "comando"` para ejecutar comandos en Asterisk
5. **Dialplan de PekePBX**: cuidado con los ficheros que editas, PekePBX puede sobreescribir. Usa `extensions_custom.conf` o similar
6. **Si hay errores de compilación**: lee el error, ajusta el código, recompila
7. **Para probar**: necesitas 3 procesos corriendo — echo_server.py, el Bridge, y hacer una llamada
8. **El protocolo AudioSocket**: tipo (1 byte) + longitud (2 bytes big-endian) + payload. Tipos: 0x00=hangup, 0x01=UUID, 0x10=audio
9. **AMI**: protocolo texto sobre TCP puerto 5038. Acciones: Redirect (transfer), Hangup, Originate
