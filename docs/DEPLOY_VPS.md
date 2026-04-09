# Guía de Despliegue en VPS
## Audio Bridge - Smartgroup Notarías

---

## 1. Crear cuenta en OpenAI y obtener API Key

1. Ve a **https://platform.openai.com/signup** y crea una cuenta
2. Ve a **https://platform.openai.com/api-keys**
3. Haz clic en **"Create new secret key"**
4. Ponle un nombre (ej. "Audio Bridge Notarías")
5. Copia la key (empieza por `sk-...`). Guárdala bien, no se puede volver a ver
6. Ve a **https://platform.openai.com/settings/organization/billing** y añade un método de pago
7. Añade créditos (con 10€ tienes para muchas pruebas)

**Importante:** El modelo `gpt-realtime` o `gpt-realtime-mini` debe estar disponible en tu cuenta. Si no lo ves, puede que necesites acceso al tier de uso adecuado.

---

## 2. Preparar la VPS

### Requisitos mínimos
- **OS:** Ubuntu 22.04 o 24.04
- **CPU:** 2 vCPU
- **RAM:** 4 GB
- **Disco:** 20 GB SSD
- **Red:** IP pública, puertos 9092 (AudioSocket), 8080 (API), 5038 (AMI)
- **Ubicación:** Europa (por latencia y GDPR)

### Proveedores recomendados (económicos)
- **Hetzner Cloud** (Falkenstein/Helsinki): CX22 = ~4€/mes
- **OVH**: VPS Starter = ~6€/mes
- **Contabo**: VPS S = ~6€/mes

### Instalación base

```bash
# Actualizar sistema
sudo apt update && sudo apt upgrade -y

# Instalar dependencias
sudo apt install -y git curl wget build-essential python3 python3-pip

# Instalar Go 1.22+
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version  # Verificar

# Instalar Docker (opcional, para despliegue con contenedores)
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
# Cerrar sesión y volver a entrar para que aplique

# Instalar pip websockets (para el echo server)
pip3 install websockets
```

---

## 3. Compilar el Audio Bridge

```bash
# Clonar o subir el proyecto
cd /opt
git clone <tu-repo> audio-bridge  # o sube el tar.gz y descomprime
cd audio-bridge

# Descargar dependencias
go mod tidy

# Compilar
go build -o audio-bridge ./cmd/bridge

# Verificar
./audio-bridge --help
```

---

## 4. Configuración

### 4.1 Configurar el Bridge

```bash
cp configs/config.yaml configs/config.local.yaml
nano configs/config.local.yaml
```

Ajustar estos valores:

```yaml
server:
  audiosocket_addr: "0.0.0.0:9092"
  max_concurrent: 50

asterisk:
  ami_host: "TU_IP_ASTERISK"      # IP de tu Asterisk
  ami_port: 5038
  ami_user: "bridge"
  ami_password: "TU_PASSWORD_AMI"

# --- Para pruebas con OpenAI ---
ai:
  endpoint: "wss://api.openai.com/v1/realtime?model=gpt-realtime-mini"
  auth_type: "bearer"
  bearer_token: "sk-TU_API_KEY_OPENAI"
  timeout_sec: 10

# --- Para pruebas con Echo Server ---
# ai:
#   endpoint: "ws://localhost:9093/v1/stream"
#   auth_type: "api_key"
#   api_key: "test"
#   timeout_sec: 5

api:
  addr: "0.0.0.0:8080"
  api_key: "TU_API_KEY_PARA_LA_API"

logging:
  level: "debug"        # usar "info" en producción
  format: "console"     # usar "json" en producción

tenants:
  - notaria_id: "N001"
    name: "Notaría de prueba"
    ddis:
      - "TU_DDI_DE_PRUEBA"
    enabled: true
    sip_trunk: "trunk-test"
    schedule:
      timezone: "Europe/Madrid"
      business_hours:
        - days: "mon-fri"
          start: "00:00"
          end: "23:59"    # 24h para pruebas
    transfers:
      default: "200"
```

### 4.2 Configurar Asterisk

En tu Asterisk, añadir al `manager.conf`:

```ini
[bridge]
secret = TU_PASSWORD_AMI
deny = 0.0.0.0/0.0.0.0
permit = TU_IP_VPS/255.255.255.255
read = system,call,log,agent,user
write = system,call,agent,user,originate
```

Verificar que el módulo AudioSocket está cargado:

```bash
asterisk -rx "module show like audiosocket"
# Debe mostrar res_audiosocket.so
# Si no aparece:
asterisk -rx "module load res_audiosocket.so"
```

Añadir al `extensions.conf` (o hacer include):

```ini
; Contexto de prueba para notarías
[notarias-ia-test]
exten => _X.,1,NoOp(=== TEST: Enviando a Bridge ===)
 same => n,Set(CALL_UUID=${SHELL(uuidgen | tr -d '\n')})
 same => n,AudioSocket(${CALL_UUID},TU_IP_VPS:9092)
 same => n,Hangup()
```

Recargar:

```bash
asterisk -rx "dialplan reload"
asterisk -rx "manager reload"
```

---

## 5. Prueba con Echo Server (Paso 1)

Esto verifica que AudioSocket → Bridge → WSS funciona.

### En la VPS, terminal 1: Echo Server

```bash
cd /opt/audio-bridge
python3 scripts/echo_server.py --port 9093
```

### En la VPS, terminal 2: Bridge (apuntando al echo server)

Asegúrate de que en config.local.yaml el `ai.endpoint` apunte a `ws://localhost:9093/v1/stream`

```bash
cd /opt/audio-bridge
./audio-bridge -config configs/config.local.yaml
```

### Desde un teléfono: hacer una llamada de prueba

Llama a un número que enrute al contexto `notarias-ia-test`. Deberías oírte a ti mismo con un ligero delay (eco). En la terminal del echo server verás los frames de audio llegando.

En la terminal del echo server, pulsa:
- `t` → envía evento de transferencia (la llamada debería transferirse)
- `h` → envía evento de hangup (la llamada se cuelga)

**Si te oyes a ti mismo, el Bridge funciona correctamente.**

---

## 6. Prueba con OpenAI Realtime (Paso 2)

Una vez verificado con el echo server, cambiamos a OpenAI.

### Cambiar config.local.yaml

```yaml
ai:
  endpoint: "wss://api.openai.com/v1/realtime?model=gpt-realtime-mini"
  auth_type: "bearer"
  bearer_token: "sk-TU_API_KEY_OPENAI"
  timeout_sec: 10
```

### Reiniciar el Bridge

```bash
# Parar el anterior (Ctrl+C) y arrancar de nuevo
./audio-bridge -config configs/config.local.yaml
```

### Llamar de nuevo

Ahora al llamar deberías poder hablar con el asistente de IA de OpenAI. El asistente:
- Responderá en español
- Se presentará como asistente de la notaría
- Podrá transferir la llamada si se lo pides
- Colgará cuando la conversación termine

**Nota sobre el formato de audio:** El adaptador de OpenAI usa G.711 a-law, que es nativo de Asterisk. Si hay problemas de audio, puede ser necesario ajustar el codec en el AudioSocket. Prueba primero y si hay distorsión, lo afinamos.

---

## 7. Systemd Service (para producción)

Crear el servicio para que el Bridge arranque automáticamente:

```bash
sudo nano /etc/systemd/system/audio-bridge.service
```

```ini
[Unit]
Description=Audio Bridge - Smartgroup Notarías
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/audio-bridge
ExecStart=/opt/audio-bridge/audio-bridge -config /opt/audio-bridge/configs/config.local.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535

# Environment
Environment=GOMAXPROCS=2

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=audio-bridge

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable audio-bridge
sudo systemctl start audio-bridge

# Ver logs
sudo journalctl -u audio-bridge -f
```

---

## 8. Firewall

```bash
# Instalar ufw si no está
sudo apt install -y ufw

# Reglas básicas
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow ssh
sudo ufw allow 9092/tcp comment "AudioSocket (Asterisk)"
sudo ufw allow 8080/tcp comment "API REST"

# Solo permitir AudioSocket desde tu Asterisk
sudo ufw delete allow 9092/tcp
sudo ufw allow from TU_IP_ASTERISK to any port 9092 proto tcp comment "AudioSocket from Asterisk"

# Solo permitir API desde IPs conocidas
sudo ufw delete allow 8080/tcp
sudo ufw allow from TU_IP_OFICINA to any port 8080 proto tcp comment "API from office"

sudo ufw enable
sudo ufw status verbose
```

---

## 9. Verificación rápida

```bash
# Health check del API
curl http://localhost:8080/health

# Debería devolver:
# {"active_calls":0,"status":"ok","timestamp":"..."}

# Ver logs en tiempo real
sudo journalctl -u audio-bridge -f

# Ver estado del servicio
sudo systemctl status audio-bridge
```

---

## 10. Troubleshooting

### El Bridge no conecta con Asterisk AMI
```bash
# Verificar que AMI está escuchando
telnet TU_IP_ASTERISK 5038
# Debe mostrar "Asterisk Call Manager/..."

# Verificar credenciales
asterisk -rx "manager show user bridge"

# Verificar que el firewall permite la conexión
```

### AudioSocket no conecta
```bash
# Verificar que el Bridge escucha en 9092
ss -tlnp | grep 9092

# Verificar desde Asterisk que llega
# En el dialplan, añadir un NoOp antes del AudioSocket:
# same => n,NoOp(Conectando a AudioSocket ${CALL_UUID})

# Ver logs del Bridge
sudo journalctl -u audio-bridge -f | grep audiosocket
```

### No se oye audio / audio distorsionado
```bash
# Verificar codec en Asterisk
# En sip.conf o pjsip.conf, asegurar que alaw está disponible:
# allow=alaw

# Verificar que el Bridge recibe frames
# Los logs deben mostrar "Audio: X frames received"

# Si hay distorsión, puede ser problema de sample rate
# Intentar cambiar input_audio_format a "pcm16" en el adaptador OpenAI
```

### OpenAI devuelve error
```bash
# Verificar API key
curl -s https://api.openai.com/v1/models \
  -H "Authorization: Bearer sk-TU_KEY" | head -20

# Verificar que tienes acceso al modelo realtime
# En https://platform.openai.com/settings/organization/limits

# Ver errores específicos en los logs del Bridge
sudo journalctl -u audio-bridge -f | grep -i error
```

---

## Resumen del orden de pruebas

1. **VPS lista** → sistema instalado, Go compilado, Bridge compilado
2. **Echo server** → verificar que AudioSocket funciona (te oyes a ti mismo)
3. **OpenAI Realtime** → hablar con el bot de IA
4. **Systemd** → Bridge como servicio permanente
5. **Firewall** → securizar accesos
6. **Producción** → cuando CTNotariado dé su endpoint, solo cambiar URL en config
