package server

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const DefaultInternetURL = "https://vortex93.github.io/fomo-party/"

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ConfigurePeers reads host-only ICE configuration. TURN credentials are never
// included in guest replies, URLs, or static GitHub Pages assets.
func (a *API) ConfigurePeers(internetURL, rawICE string) error {
	u, err := url.Parse(internetURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("internet-url must be an HTTPS guest page URL, including its trailing slash")
	}
	if !strings.HasSuffix(u.Path, "/") {
		return errors.New("internet-url must end with / for relative assets")
	}
	servers := []ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	if rawICE != "" {
		if len(rawICE) > 16384 {
			return errors.New("FOMO_ICE_SERVERS is too large")
		}
		if err := json.Unmarshal([]byte(rawICE), &servers); err != nil || len(servers) == 0 || len(servers) > 8 {
			return errors.New("FOMO_ICE_SERVERS must be a JSON array of 1–8 ICE servers with urls arrays")
		}
		for _, server := range servers {
			if len(server.URLs) == 0 || len(server.URLs) > 8 || len(server.Username) > 1024 || len(server.Credential) > 2048 {
				return errors.New("invalid ICE server configuration")
			}
			for _, raw := range server.URLs {
				parsed, err := url.Parse(raw)
				if err != nil || len(raw) > 1024 || parsed.Opaque == "" || parsed.Fragment != "" || parsed.Host != "" {
					return errors.New("ICE URLs must use stun:, stuns:, turn:, or turns:")
				}
				switch parsed.Scheme {
				case "stun", "stuns", "turn", "turns":
				default:
					return errors.New("ICE URLs must use stun:, stuns:, turn:, or turns:")
				}
				host, err := url.Parse("//" + parsed.Opaque)
				if err != nil || host.Hostname() == "" || host.User != nil || host.Path != "" {
					return errors.New("invalid ICE server host")
				}
				query, err := url.ParseQuery(parsed.RawQuery)
				if err != nil || len(query) > 1 || (len(query) == 1 && (len(query["transport"]) != 1 || (query.Get("transport") != "udp" && query.Get("transport") != "tcp"))) {
					return errors.New("invalid ICE transport")
				}
				if strings.HasPrefix(parsed.Scheme, "turn") && (server.Username == "" || server.Credential == "") {
					return errors.New("TURN requires username and credential")
				}
			}
		}
	}
	a.internetURL, a.iceServers = internetURL, servers
	return nil
}
