# Guía de Pruebas en Local — PekePBX (CentOS/Rocky)
## Audio Bridge - Smartgroup Notarías

---

## Paso 1 — Instalar Go en la máquina de la PBX

```bash
# Descargar Go 1.22
cd /tmp
curl -OL https://go.dev/dl/go1.22.5.linux-amd64.tar.gz

# Instalar
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz

# Añadir al PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verificar
go version
# Debe mostrar: go version go1.22.5 linux/amd64
```

---

## Paso 2 — Subir y compilar el Bridge

```bash
# Crear directorio
mkdir -p /opt/audio-bridge
cd /opt/audio-bridge

# Subir el tar.gz (desde tu PC con scp)
# scp audio-bridge-complete.tar.gz root@IP_PBX:/opt/

# O si ya lo tienes en la máquina, descomprimir
cd /opt
tar -xzf audio-bridge-complete.tar.gz
cd audio-bridge

# Descargar dependencias e instalar
go mod tidy

# Si go mod tidy da error de red, puede que necesites:
# export GOPROXY=https://proxy.golang.org,direct

# Compilar
go build -o audio-bridge ./cmd/bridge

# Verificar que se compiló
ls -la audio-bridge
# Debe mostrar un binario de unos 15-20 MB
```

---

## Paso 3 — Verificar AudioSocket en tu PekePBX

PekePBX usa Asterisk por debajo. Hay que comprobar que el módulo AudioSocket está disponible.

```bash
# Verificar si el módulo existe
asterisk -rx "module show like audiosocket"

# Si aparece res_audiosocket.so → perfecto, ya está cargado
# Si NO aparece, intentar cargarlo:
asterisk -rx "module load res_audiosocket.so"

# Si da error de que no existe el módulo, hay que instalarlo.
# En CentOS/Rocky con Asterisk 16+:
yum list installed | grep asterisk
# Ver qué versión tienes

# Si el módulo no está compilado, puedes compilarlo aparte
# o actualizar Asterisk a una versión que lo incluya.
# AudioSocket viene incluido de serie desde Asterisk 16.
```

Si tu Asterisk no tiene AudioSocket, hay una alternativa que vemos luego. Pero normalmente en PekePBX con Asterisk 16+ ya viene.

---

## Paso 4 — Configurar AMI para el Bridge

El Bridge se va a conectar a Asterisk por AMI para controlar las llamadas. Como todo está en la misma máquina, usamos localhost.

```bash
# Editar manager.conf
nano /etc/asterisk/manager.conf
```

Añadir al final:

```ini
[bridge]
secret = bridge_test_2026
deny = 0.0.0.0/0.0.0.0
permit = 127.0.0.1/255.255.255.255
read = system,call,log,agent,user
write = system,call,agent,user,originate
```

```bash
# Recargar AMI
asterisk -rx "manager reload"

# Verificar que el usuario existe
asterisk -rx "manager show user bridge"

# Probar la conexión manualmente
telnet 127.0.0.1 5038
# Debe mostrar: Asterisk Call Manager/X.X.X
# Escribe: Action: Login
#          Username: bridge
#          Secret: bridge_test_2026
#          (línea vacía)
# Debe responder: Response: Success
# Escribe: Action: Logoff
#          (línea vacía)
# Ctrl+] y luego quit para salir
```

---

## Paso 5 — Configurar el Bridge

```bash
cd /opt/audio-bridge
cp configs/config.yaml configs/config.local.yaml
nano configs/config.local.yaml
```

Para la primera prueba con echo server, usa esta configuración:

```yaml
server:
  audiosocket_addr: "0.0.0.0:9092"
  max_concurrent: 10

asterisk:
  ami_host: "127.0.0.1"
  ami_port: 5038
  ami_user: "bridge"
  ami_password: "bridge_test_2026"

# Apuntamos al echo server local para la primera prueba
ai:
  endpoint: "ws://127.0.0.1:9093/v1/stream"
  auth_type: "api_key"
  api_key: "test"
  timeout_sec: 5

audio:
  sample_rate: 16000
  bit_depth: 16
  channels: 1
  codec: "pcm_s16le"
  frame_size_ms: 20

api:
  addr: "0.0.0.0:8080"
  api_key: "test_api_key_2026"

logging:
  level: "debug"
  format: "console"

tenants:
  - notaria_id: "TEST01"
    name: "Notaria de prueba"
    ddis:
      - "100"          # Pon aquí la extensión o DDI que uses para probar
    enabled: true
    sip_trunk: ""
    schedule:
      timezone: "Europe/Madrid"
      business_hours:
        - days: "mon-sun"
          start: "00:00"
          end: "23:59"
    transfers:
      default: "200"   # Extensión a la que transferir en fallback
```

---

## Paso 6 — Añadir extensión de prueba en PekePBX

Necesitas una extensión/número que al llamarla, envíe la llamada al Bridge por AudioSocket.

### Opción A: Por el dialplan directamente

```bash
nano /etc/asterisk/extensions_custom.conf
# (o el fichero donde PekePBX permita custom dialplan)
```

Añadir:

```ini
[notarias-test]
exten => 899,1,NoOp(=== TEST BRIDGE: AudioSocket ===)
 same => n,Answer()
 same => n,Wait(1)
 same => n,Set(CALL_UUID=${SHELL(uuidgen | tr -d '\n')})
 same => n,NoOp(UUID: ${CALL_UUID})
 same => n,AudioSocket(${CALL_UUID},127.0.0.1:9092)
 same => n,NoOp(=== AudioSocket finalizado ===)
 same => n,Hangup()
```

Luego necesitas que la extensión 899 sea accesible. En PekePBX puedes crear un "Misc Destination" o enrutar desde un IVR/DID al contexto `notarias-test`. O si tienes acceso al dialplan del contexto de extensiones internas (normalmente `from-internal`), puedes incluir:

```ini
; En extensions.conf o en el contexto from-internal
include => notarias-test
```

### Opción B: Crear una extensión custom en PekePBX

Si PekePBX tiene un fichero de extensiones custom (como `extensions_override_freepbx.conf` o similar), añade ahí el contexto y haz un include.

```bash
# Recargar dialplan
asterisk -rx "dialplan reload"

# Verificar que la extensión existe
asterisk -rx "dialplan show notarias-test"
# Debe mostrar la extensión 899
```

---

## Paso 7 — Instalar Python websockets (para el echo server)

```bash
# En CentOS/Rocky
pip3 install websockets

# Si pip3 no está:
yum install -y python3-pip
pip3 install websockets
```

---

## Paso 8 — ¡PRUEBA! Echo Server

Abre **3 terminales** SSH a tu PBX:

### Terminal 1: Echo Server

```bash
cd /opt/audio-bridge
python3 scripts/echo_server.py --port 9093
```

Verás:
```
  Echo WSS Server starting on ws://0.0.0.0:9093
  Waiting for Bridge connections...
```

### Terminal 2: Bridge

```bash
cd /opt/audio-bridge
./audio-bridge -config configs/config.local.yaml
```

Verás:
```
INFO  Audio Bridge starting  audiosocket_addr=0.0.0.0:9092 api_addr=0.0.0.0:8080
INFO  Connected to Asterisk AMI
INFO  AudioSocket server started  addr=0.0.0.0:9092
INFO  API server starting  addr=0.0.0.0:8080
INFO  Audio Bridge is running. Press Ctrl+C to stop.
```

### Terminal 3: Verificar y llamar

```bash
# Verificar que todo está escuchando
ss -tlnp | grep -E "9092|9093|8080"
# Debe mostrar los 3 puertos

# Health check
curl http://127.0.0.1:8080/health
# {"active_calls":0,"status":"ok","timestamp":"..."}
```

Ahora **llama a la extensión 899** desde un teléfono registrado en tu PekePBX.

### Qué esperar:

1. El teléfono marca 899
2. PekePBX ejecuta el dialplan → AudioSocket conecta con el Bridge
3. En Terminal 2 (Bridge) ves: `AudioSocket connection established uuid=...`
4. En Terminal 1 (Echo) ves: `NEW CONNECTION #1` y los frames de audio
5. **Te oyes a ti mismo** con un pequeño delay
6. En Terminal 1, pulsa `t` → la llamada se transfiere a la extensión 200
7. O pulsa `h` → la llamada se cuelga

**SI TE OYES: TODO FUNCIONA. El Bridge está listo.**

---

## Paso 9 — Prueba con OpenAI Realtime

Una vez que el echo funciona, solo hay que cambiar la URL.

### Editar config

```bash
nano /opt/audio-bridge/configs/config.local.yaml
```

Cambiar la sección `ai:`:

```yaml
ai:
  endpoint: "wss://api.openai.com/v1/realtime?model=gpt-realtime-mini"
  auth_type: "bearer"
  bearer_token: "sk-PEGA_AQUI_TU_API_KEY_DE_OPENAI"
  timeout_sec: 10
```

### Reiniciar el Bridge

```bash
# Ctrl+C en Terminal 2 para parar el Bridge
# Puedes cerrar Terminal 1 (echo server ya no se necesita)

# Arrancar de nuevo
cd /opt/audio-bridge
./audio-bridge -config configs/config.local.yaml
```

### Llamar de nuevo a la extensión 899

Ahora al hablar deberías escuchar al asistente de IA responderte en español. Prueba a decirle cosas como:

- "Hola, quiero pedir cita para una escritura"
- "¿Cuál es el horario de la notaría?"
- "Quiero hablar con el notario" (debería intentar transferir)

---

## Paso 10 — Mover a VPS (cuando funcione todo)

Una vez que estés contento con las pruebas locales:

```bash
# En tu PBX, compilar para Linux (por si la VPS es distinta)
cd /opt/audio-bridge
GOOS=linux GOARCH=amd64 go build -o audio-bridge-linux ./cmd/bridge

# Subir a la VPS
scp audio-bridge-linux root@IP_VPS:/opt/audio-bridge/audio-bridge
scp configs/config.local.yaml root@IP_VPS:/opt/audio-bridge/configs/
scp scripts/echo_server.py root@IP_VPS:/opt/audio-bridge/scripts/
scp configs/dialplan-notarias.conf root@IP_VPS:/opt/audio-bridge/configs/

# En la VPS, ajustar config.local.yaml:
# - ami_host: IP de tu Asterisk de producción (ya no es 127.0.0.1)
# - Ajustar DDIs de las notarías reales
# - Ajustar extensiones de transferencia reales

# En tu Asterisk de producción:
# - Añadir el usuario AMI 'bridge' con permit = IP_VPS
# - Añadir el dialplan de notarias apuntando a IP_VPS:9092
# - dialplan reload + manager reload

# En la VPS, seguir la guía DEPLOY_VPS.md para systemd y firewall
```

---

## Resumen de puertos

| Puerto | Servicio | Quién conecta |
|--------|----------|---------------|
| 9092   | AudioSocket | Asterisk → Bridge |
| 9093   | Echo Server | Bridge → Echo (solo pruebas) |
| 8080   | API REST | Tú / sistemas externos → Bridge |
| 5038   | AMI | Bridge → Asterisk |

---

## Troubleshooting rápido

### "module load res_audiosocket.so" da error
```bash
# Verificar versión de Asterisk
asterisk -rx "core show version"
# Necesitas Asterisk 16+. Si tienes 13 o inferior, hay que actualizar.

# Buscar el módulo
find / -name "res_audiosocket.so" 2>/dev/null
# Si no existe, compilar o instalar el paquete correspondiente
```

### Bridge conecta pero no se oye audio
```bash
# Verificar que Asterisk envía audio por AudioSocket
# En el CLI de Asterisk durante la llamada:
asterisk -rx "core show channels verbose"
# Debe mostrar el canal con Application=AudioSocket

# Verificar codecs
asterisk -rx "core show codecs"
# slin16 (signed linear 16kHz) debe estar disponible
```

### "Connection refused" al conectar AMI
```bash
# Verificar que AMI escucha
ss -tlnp | grep 5038

# Si no escucha, editar manager.conf:
# [general]
# enabled = yes
# port = 5038
# bindaddr = 127.0.0.1

asterisk -rx "manager reload"
```

### Echo server no recibe conexiones
```bash
# Verificar que el echo server está corriendo
ss -tlnp | grep 9093

# Verificar logs del Bridge - debe mostrar:
# "Connecting to AI module" url=ws://127.0.0.1:9093/v1/stream

# Si el Bridge no conecta al echo, puede ser firewall local:
# (en CentOS/Rocky, firewalld puede bloquear localhost)
firewall-cmd --zone=trusted --add-source=127.0.0.1 --permanent
firewall-cmd --reload
```
