package operatorpasskey

import (
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	recoveryTicketLifetime = 5 * time.Minute
	maxRecoveryTickets      = 128
)

type recoveryTicket struct {
	UserID    string
	ExpiresAt time.Time
}

var recoveryTickets = struct {
	sync.Mutex
	items map[[32]byte]recoveryTicket
}{items: make(map[[32]byte]recoveryTicket)}

func IssueRecoveryTicket(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New("recovery user is missing")
	}
	token, err := randomToken(flowTokenBytes)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	recoveryTickets.Lock()
	defer recoveryTickets.Unlock()
	cleanupRecoveryTicketsLocked(now)
	if len(recoveryTickets.items) >= maxRecoveryTickets {
		return "", errors.New("too many active recovery tickets")
	}
	recoveryTickets.items[sha256.Sum256([]byte(token))] = recoveryTicket{UserID: userID, ExpiresAt: now.Add(recoveryTicketLifetime)}
	return token, nil
}

func ConsumeRecoveryTicket(token, userID string) bool {
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	recoveryTickets.Lock()
	defer recoveryTickets.Unlock()
	cleanupRecoveryTicketsLocked(now)
	ticket, ok := recoveryTickets.items[digest]
	delete(recoveryTickets.items, digest)
	return ok && ticket.UserID == strings.TrimSpace(userID) && ticket.ExpiresAt.After(now)
}

func cleanupRecoveryTicketsLocked(now time.Time) {
	for digest, ticket := range recoveryTickets.items {
		if !ticket.ExpiresAt.After(now) {
			delete(recoveryTickets.items, digest)
		}
	}
}
