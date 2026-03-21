package hostinfo

import "strings"

type Snapshot struct {
	RoamingAppDataPath string `json:"roaming_app_data_path,omitempty"`
	CashServerProduct  string `json:"cash_server_product,omitempty"`
	CashServerConfig   string `json:"cash_server_config,omitempty"`
	CashServerURL      string `json:"cash_server_url,omitempty"`
	TeamviewerID       string `json:"teamviewer_id,omitempty"`
	AnydeskID          string `json:"anydesk_id,omitempty"`
	LitemanagerID      string `json:"litemanager_id,omitempty"`
	RustdeskID         string `json:"rustdesk_id,omitempty"`
}

func (s Snapshot) Empty() bool {
	return strings.TrimSpace(s.RoamingAppDataPath) == "" &&
		strings.TrimSpace(s.CashServerProduct) == "" &&
		strings.TrimSpace(s.CashServerConfig) == "" &&
		strings.TrimSpace(s.CashServerURL) == "" &&
		strings.TrimSpace(s.TeamviewerID) == "" &&
		strings.TrimSpace(s.AnydeskID) == "" &&
		strings.TrimSpace(s.LitemanagerID) == "" &&
		strings.TrimSpace(s.RustdeskID) == ""
}
