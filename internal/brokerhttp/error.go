package brokerhttp

import (
	"fmt"
	"net/http"
)

type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string {
	return e.message
}

func writeError(w http.ResponseWriter, err error) {
	if he, ok := err.(*httpError); ok {
		http.Error(w, he.message, he.status)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func badRequest(msg string) error {
	return &httpError{status: http.StatusBadRequest, message: msg}
}

func unauthorized() error {
	return &httpError{status: http.StatusUnauthorized, message: "unauthorized"}
}

func forbidden() error {
	return &httpError{status: http.StatusForbidden, message: "forbidden"}
}

func notFound(msg string) error {
	return &httpError{status: http.StatusNotFound, message: msg}
}

func githubAPIError() error {
	return &httpError{status: http.StatusBadGateway, message: "github api error"}
}

func listenMustBeLoopback(addr string) error {
	return fmt.Errorf("listen must be 127.0.0.1 (got %q)", addr)
}
