package membership

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// MembershipDTO is what GET /me/membership answers.
type MembershipDTO struct {
	State   State  `json:"state"`
	LocalID string `json:"localId"`
	Address string `json:"address,omitempty"`
}

// IdentifierHandler answers whether the last path segment is available under
// the owned domain: 204 free, 400 invalid or reserved, 409 taken. Mount it
// publicly, rate-limited by the product, as GET /machine/identifiers/{local}.
func (s *Service) IdentifierHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		local := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch err := s.Available(r.Context(), local); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrTaken):
			problem(w, http.StatusConflict, "taken")
		case errors.Is(err, ErrReserved):
			problem(w, http.StatusBadRequest, "reserved")
		case errors.Is(err, ErrInvalidIdentifier):
			problem(w, http.StatusBadRequest, "invalid")
		default:
			problem(w, http.StatusServiceUnavailable, "unavailable")
		}
	})
}

// MeHandler serves the signed-in person's membership: GET answers the
// MembershipDTO, DELETE asks for the erasure and answers 202. identity reads
// the person the product's middleware verified; false is 401.
func (s *Service) MeHandler(identity func(*http.Request) (Identity, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identity(r)
		if !ok || id.ID == "" {
			problem(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		switch r.Method {
		case http.MethodGet:
			m, err := s.Resolve(r.Context(), id)
			switch {
			case errors.Is(err, ErrErased):
				problem(w, http.StatusForbidden, "erased")
			case errors.Is(err, ErrConflict):
				problem(w, http.StatusConflict, "conflict")
			case err != nil:
				problem(w, http.StatusServiceUnavailable, "unavailable")
			default:
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(MembershipDTO{State: m.State, LocalID: m.LocalID, Address: m.Address})
			}
		case http.MethodDelete:
			if err := s.RequestErase(r.Context(), id.ID); err != nil {
				problem(w, http.StatusServiceUnavailable, "unavailable")
				return
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
