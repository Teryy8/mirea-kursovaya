package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
)

type dashboardAccess struct {
	username [sha256.Size]byte
	password [sha256.Size]byte
}

func loadDashboardAccess() (*dashboardAccess, error) {
	filename := os.Getenv("LIVETRAFFIC_DASHBOARD_PASSWORD_FILE")
	if filename == "" {
		return nil, nil
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	password := strings.TrimRight(string(content), "\r\n")
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("dashboard password file is empty")
	}
	username := os.Getenv("LIVETRAFFIC_DASHBOARD_USER")
	if username == "" {
		username = "analyst"
	}
	if strings.Contains(username, ":") {
		return nil, errors.New("dashboard username must not contain a colon")
	}
	return &dashboardAccess{username: sha256.Sum256([]byte(username)), password: sha256.Sum256([]byte(password))}, nil
}

func (a *dashboardAccess) protect(next http.HandlerFunc) http.HandlerFunc {
	if a == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		userHash, passwordHash := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(password))
		match := subtle.ConstantTimeCompare(userHash[:], a.username[:]) &
			subtle.ConstantTimeCompare(passwordHash[:], a.password[:])
		if !ok || match != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="LiveTraffic", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "dashboard authentication required")
			return
		}
		next(w, r)
	}
}
