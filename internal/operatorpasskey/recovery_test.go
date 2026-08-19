package operatorpasskey

import "testing"

func TestRecoveryTicketsAreBoundedAndOneTime(t *testing.T) {
	recoveryTickets.Lock()
	recoveryTickets.items = make(map[[32]byte]recoveryTicket)
	recoveryTickets.Unlock()
	t.Cleanup(func() {
		recoveryTickets.Lock()
		recoveryTickets.items = make(map[[32]byte]recoveryTicket)
		recoveryTickets.Unlock()
	})

	first := ""
	for index := 0; index < maxRecoveryTickets; index++ {
		token, err := IssueRecoveryTicket("u_test")
		if err != nil {
			t.Fatalf("ticket %d: %v", index, err)
		}
		if index == 0 {
			first = token
		}
	}
	if _, err := IssueRecoveryTicket("u_test"); err == nil {
		t.Fatal("recovery ticket store accepted an entry beyond its configured bound")
	}
	if !ConsumeRecoveryTicket(first, "u_test") {
		t.Fatal("valid recovery ticket was rejected")
	}
	if ConsumeRecoveryTicket(first, "u_test") {
		t.Fatal("recovery ticket was reusable")
	}
	if _, err := IssueRecoveryTicket("u_test"); err != nil {
		t.Fatalf("consuming a recovery ticket did not free capacity: %v", err)
	}
}
