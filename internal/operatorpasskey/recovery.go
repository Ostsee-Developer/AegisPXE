package operatorpasskey

import (
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"time"
)

const recoveryTicketLifetime = 5 * time.Minute

type recoveryTicket struct {
	UserID    string
	ExpiresAt time.Time
}

var recoveryTicketStore sync.Map

func IssueRecoveryTicket(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New("recovery user is missing")
	}
	token, err := randomToken(flowTokenBytes)
	if err != nil {
		return "", err
	}
	recoveryTicketStore.Store(sha256.Sum256([]byte(token)), recoveryTicket{UserID: userID, ExpiresAt: time.Now().UTC().Add(recoveryTicketLifetime)})
	return token, nil
}

func ConsumeRecoveryTicket(token, userID string) bool {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	value, ok := recoveryTicketStore.LoadAndDelete(digest)
	if !ok {
		return false
	}
	ticket, ok := value.(recoveryTicket)
	return ok && ticket.UserID == strings.TrimSpace(userID) && ticket.ExpiresAt.After(time.Now().UTC())
}
