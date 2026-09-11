package brokerhttp

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/registrar"
)

type Env struct {
	GitHubAppID         string
	GitHubAppPrivateKey string
	SigningKeyPEM       string
	JWTIssuer           string
	JWTVersion          string
	GitHubAPI           string
	HTTPClient          *http.Client
}

func Handler(env Env) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(registrar.BrokerJITConfigPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := handleJIT(env, w, r); err != nil {
			writeError(w, err)
		}
	})
	mux.HandleFunc(registrar.BrokerRunnersListPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := handleList(env, w, r); err != nil {
			writeError(w, err)
		}
	})
	mux.HandleFunc(hostcredential.CredentialExchangePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := handleExchange(env, w, r); err != nil {
			writeError(w, err)
		}
	})
	mux.HandleFunc("/v1/runners/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := handleDelete(env, w, r); err != nil {
			writeError(w, err)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	return mux
}

func handleExchange(env Env, w http.ResponseWriter, r *http.Request) error {
	userToken, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return unauthorized()
	}
	body, err := readJSON(r)
	if err != nil {
		return err
	}
	tgt, err := parseOrgTarget(body["target"])
	if err != nil {
		return err
	}
	installationID, err := findAppInstallation(env, userToken, tgt.Org)
	if err != nil {
		return err
	}
	cred, err := issueHostJWT(env, installationID, tgt)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"credential": cred,
		"target": map[string]any{
			"type":            "org",
			"org":             tgt.Org,
			"runner_group_id": tgt.RunnerGroupID,
		},
	})
}

func withBrokerAuth(env Env, r *http.Request) (orgTarget, string, map[string]any, error) {
	installationID, credTarget, err := hostCredentialFromAuth(env, r.Header.Get("Authorization"))
	if err != nil {
		return orgTarget{}, "", nil, err
	}
	body, err := readJSON(r)
	if err != nil {
		return orgTarget{}, "", nil, err
	}
	requested, err := parseOrgTarget(body["target"])
	if err != nil {
		return orgTarget{}, "", nil, err
	}
	if targetKey(credTarget) != targetKey(requested) {
		return orgTarget{}, "", nil, forbidden()
	}
	token, err := createInstallationToken(env, installationID)
	if err != nil {
		return orgTarget{}, "", nil, err
	}
	return requested, token, body, nil
}

func handleJIT(env Env, w http.ResponseWriter, r *http.Request) error {
	tgt, token, body, err := withBrokerAuth(env, r)
	if err != nil {
		return err
	}
	name, _ := body["name"].(string)
	labels := stringSlice(body["labels"])
	jit, err := createJIT(env, token, tgt, labels, name)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"encoded_jit_config": jit["encoded_jit_config"],
		"runner":             jit["runner"],
	})
}

func handleList(env Env, w http.ResponseWriter, r *http.Request) error {
	tgt, token, body, err := withBrokerAuth(env, r)
	if err != nil {
		return err
	}
	prefix, _ := body["prefix"].(string)
	runners, err := listRunners(env, token, tgt, prefix)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"runners": runners})
}

func handleDelete(env Env, w http.ResponseWriter, r *http.Request) error {
	idStr := strings.TrimPrefix(r.URL.Path, "/v1/runners/")
	runnerID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || runnerID <= 0 {
		return badRequest("bad request")
	}
	tgt, token, _, err := withBrokerAuth(env, r)
	if err != nil {
		return err
	}
	if err := deleteRunner(env, token, tgt, runnerID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func readJSON(r *http.Request) (map[string]any, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, badRequest("invalid json")
	}
	if len(bytesTrim(data)) == 0 {
		return nil, badRequest("invalid json")
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, badRequest("invalid json")
	}
	return body, nil
}

func bytesTrim(data []byte) []byte {
	return []byte(strings.TrimSpace(string(data)))
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func stringSlice(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, _ := item.(string)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
