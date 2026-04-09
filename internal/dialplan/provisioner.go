package dialplan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/smartgroup/audio-bridge/internal/ami"
	"github.com/smartgroup/audio-bridge/internal/config"
)

const (
	// DefaultBasePath is where PekePBX expects custom dialplan files
	DefaultBasePath = "/opt/sipdoc/peke-system/etc/asterisk/custom/dialplan"

	// FileSuffix distinguishes audio-bridge generated files from manual ones
	FileSuffix = "_audiobridge.conf"
)

// validID matches only alphanumeric and underscore/hyphen to prevent path traversal
var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Provisioner handles dialplan file generation and AMI reload
type Provisioner struct {
	basePath string
	logger   *zap.Logger
}

// NewProvisioner creates a new dialplan provisioner.
// If basePath is empty, DefaultBasePath is used.
func NewProvisioner(basePath string, logger *zap.Logger) *Provisioner {
	if basePath == "" {
		basePath = DefaultBasePath
	}
	return &Provisioner{basePath: basePath, logger: logger}
}

// Provision generates the dialplan file for a tenant and reloads Asterisk.
// Returns nil if the tenant has no CompanyID (nothing to provision).
func (p *Provisioner) Provision(tenant config.TenantConfig, apiKey string, amiClient *ami.Client) error {
	if tenant.CompanyID == "" {
		return nil
	}
	if !validID.MatchString(tenant.CompanyID) {
		return fmt.Errorf("invalid company_id %q: must be alphanumeric", tenant.CompanyID)
	}
	if len(tenant.DDIs) == 0 {
		p.logger.Warn("Tenant has company_id but no DDIs — dialplan will only have fallback",
			zap.String("company_id", tenant.CompanyID),
			zap.String("notaria_id", tenant.NotariaID))
	}

	content := renderDialplan(tenant, apiKey)
	filePath := filepath.Join(p.basePath, tenant.CompanyID+FileSuffix)

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing dialplan %s: %w", filePath, err)
	}
	p.logger.Info("Dialplan file written",
		zap.String("path", filePath),
		zap.String("company_id", tenant.CompanyID),
		zap.Int("ddis", len(tenant.DDIs)))

	if amiClient != nil {
		if err := amiClient.DialplanReload(); err != nil {
			p.logger.Warn("Dialplan reload failed (file was written, reload manually)",
				zap.Error(err))
			return fmt.Errorf("dialplan reload: %w", err)
		}
	}

	return nil
}

// Deprovision removes the dialplan file for a company and reloads Asterisk.
func (p *Provisioner) Deprovision(companyID string, amiClient *ami.Client) error {
	if companyID == "" {
		return nil
	}
	if !validID.MatchString(companyID) {
		return fmt.Errorf("invalid company_id %q", companyID)
	}

	filePath := filepath.Join(p.basePath, companyID+FileSuffix)

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing dialplan %s: %w", filePath, err)
	}
	p.logger.Info("Dialplan file removed", zap.String("path", filePath))

	if amiClient != nil {
		if err := amiClient.DialplanReload(); err != nil {
			p.logger.Warn("Dialplan reload failed after removal", zap.Error(err))
			return fmt.Errorf("dialplan reload: %w", err)
		}
	}

	return nil
}

// renderDialplan generates the Asterisk dialplan content for a tenant.
// The dialplan first queries the bridge routing endpoint to decide if the call
// should go to AI, be transferred (VIP), or play after-hours audio.
func renderDialplan(t config.TenantConfig, apiKey string) string {
	var sb strings.Builder

	sb.WriteString("; =============================================================================\n")
	sb.WriteString(fmt.Sprintf("; Audio Bridge — Auto-generated dialplan for %s (company %s)\n", t.Name, t.CompanyID))
	sb.WriteString("; DO NOT EDIT — regenerated automatically from the admin panel\n")
	sb.WriteString("; =============================================================================\n\n")

	sb.WriteString(fmt.Sprintf("[sub_custom_%s_pstn_in]\n", t.CompanyID))
	sb.WriteString(fmt.Sprintf("; Audio Bridge IA para notaria %s (%s)\n", t.NotariaID, t.Name))
	sb.WriteString("; Variables PekePBX: ${CALLER}, ${CALLEE}, ${DST_COMPANY}\n")
	sb.WriteString("; Routing: consulta al bridge antes de decidir AI/VIP/closed/direct\n\n")

	// One exten block per DDI
	for _, ddi := range t.DDIs {
		sb.WriteString(fmt.Sprintf("exten => %s,1,NoOp(=== Audio Bridge IA - Tenant ${DST_COMPANY} DDI %s ===)\n", ddi, ddi))
		sb.WriteString(fmt.Sprintf(" same => n,Set(NOTARIA_ID=%s)\n", t.NotariaID))
		sb.WriteString(fmt.Sprintf(" same => n,Set(AB_API_KEY=%s)\n", apiKey))
		sb.WriteString(" same => n,Set(AB_HOST=127.0.0.1:8080)\n")
		sb.WriteString(" same => n,Set(AB_SOCKET=127.0.0.1:9092)\n")

		// Step 1: Query routing endpoint (response: action|notaria_id|transfer_dest|schedule)
		sb.WriteString(` same => n,Set(ROUTE=${SHELL(curl -s "http://${AB_HOST}/api/v1/routing/check?ddi=${CALLEE}&caller_id=${CALLER}" -H "X-API-Key: ${AB_API_KEY}")})`)
		sb.WriteString("\n")
		sb.WriteString(" same => n,Set(ROUTE=${FILTER(a-zA-Z0-9\\|_.,${ROUTE})})\n")
		sb.WriteString(" same => n,Set(ACTION=${CUT(ROUTE,|,1)})\n")
		sb.WriteString(" same => n,Set(TRANSFER_DEST=${CUT(ROUTE,|,3)})\n")
		sb.WriteString(" same => n,NoOp(Routing: action=${ACTION} dest=${TRANSFER_DEST})\n")

		// Step 2: Route based on action
		sb.WriteString(fmt.Sprintf(" same => n,GotoIf($[\"${ACTION}\" = \"vip\"]?vip_%s)\n", ddi))
		sb.WriteString(fmt.Sprintf(" same => n,GotoIf($[\"${ACTION}\" = \"closed\"]?closed_%s)\n", ddi))
		sb.WriteString(fmt.Sprintf(" same => n,GotoIf($[\"${ACTION}\" = \"direct\"]?direct_%s)\n", ddi))

		// Default: send to AI
		sb.WriteString(fmt.Sprintf(" same => n(ai_%s),Set(CALL_UUID=${SHELL(uuidgen)})\n", ddi))
		sb.WriteString(" same => n,Set(CALL_UUID=${FILTER(a-z0-9-,${CALL_UUID})})\n")
		sb.WriteString(` same => n,Set(CURL_RESULT=${SHELL(curl -s -X POST http://${AB_HOST}/api/v1/calls/precreate -H "X-API-Key: ${AB_API_KEY}" -H "Content-Type: application/x-www-form-urlencoded" -d "uuid=${CALL_UUID}&notaria_id=${NOTARIA_ID}&caller_id=${CALLER}&ddi=${CALLEE}&channel=${CHANNEL}")})`)
		sb.WriteString("\n")
		sb.WriteString(" same => n,NoOp(Precreate result: ${CURL_RESULT})\n")
		sb.WriteString(" same => n,AudioSocket(${CALL_UUID},${AB_SOCKET})\n")
		sb.WriteString(" same => n,Hangup()\n")

		// VIP: direct transfer to default extension
		sb.WriteString(fmt.Sprintf(" same => n(vip_%s),NoOp(=== VIP Caller ${CALLER} ===)\n", ddi))
		sb.WriteString(" same => n,Dial(SIP/${TRANSFER_DEST},30)\n")
		sb.WriteString(" same => n,Hangup()\n")

		// Closed: after hours
		sb.WriteString(fmt.Sprintf(" same => n(closed_%s),NoOp(=== Fuera de horario ===)\n", ddi))
		sb.WriteString(" same => n,Playback(custom/fuera-horario)\n")
		sb.WriteString(" same => n,Hangup()\n")

		// Direct: bypass AI, transfer directly
		sb.WriteString(fmt.Sprintf(" same => n(direct_%s),NoOp(=== Desvio directo ===)\n", ddi))
		sb.WriteString(" same => n,Dial(SIP/${TRANSFER_DEST},30)\n")
		sb.WriteString(" same => n,Hangup()\n\n")
	}

	// Fallback: DDIs not matching pass through normally
	sb.WriteString("; Fallback: DDIs no configurados siguen dialplan normal\n")
	sb.WriteString("exten => _X.,1,Return()\n")

	return sb.String()
}
