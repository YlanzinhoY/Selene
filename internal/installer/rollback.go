package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/transaction"
)

type RollbackResult struct {
	TransactionID string `json:"transaction_id"`
	Description   string `json:"description"`
	State         string `json:"state"`
}

// History lists installation transactions newest first.
func History(env planner.Environment) ([]transaction.Journal, error) {
	return transaction.List(filepath.Join(env.XDGStateHome, "selene"))
}

// Rollback restores one transaction, or the newest restorable transaction
// when id is empty. It never executes a retained copy of an upstream script.
func Rollback(ctx context.Context, env planner.Environment, id string, output io.Writer) (RollbackResult, error) {
	if runtime.GOOS != "linux" || env.OS != "linux" {
		return RollbackResult{}, errors.New("rollback is supported only on Linux")
	}
	if effectiveUID() == 0 {
		return RollbackResult{}, errors.New("do not run Selene as root")
	}
	if output == nil {
		output = io.Discard
	}
	stateRoot := filepath.Join(env.XDGStateHome, "selene")
	lock, err := acquireInstallLock(stateRoot)
	if err != nil {
		return RollbackResult{}, err
	}
	defer lock.release()

	tx, err := selectTransaction(stateRoot, id)
	if err != nil {
		return RollbackResult{}, err
	}
	if !strings.HasPrefix(tx.Journal.Description, "install ") {
		return RollbackResult{}, errors.New("the selected journal is not a Selene installation transaction")
	}

	fmt.Fprintf(output, "Selene: restaurando o snapshot %s...\n", tx.Journal.ID)
	select {
	case <-ctx.Done():
		return RollbackResult{}, ctx.Err()
	default:
	}
	// Once restoration starts it must finish even if the terminal receives
	// Ctrl-C; interrupting a rollback can leave launchers half-restored.
	cleanupContext, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stopGuardian(cleanupContext, env)
	if err := tx.Rollback(); err != nil {
		return RollbackResult{}, fmt.Errorf("rollback %s failed: %w; journal: %s", tx.Journal.ID, err, filepath.Join(tx.Journal.Root, "journal.json"))
	}
	restoreGuardian(cleanupContext, env)
	return RollbackResult{
		TransactionID: tx.Journal.ID,
		Description:   tx.Journal.Description,
		State:         string(tx.Journal.State),
	}, nil
}

func selectTransaction(stateRoot, id string) (*transaction.Transaction, error) {
	if id != "" && id != "latest" {
		tx, err := transaction.Open(stateRoot, id)
		if err != nil {
			return nil, err
		}
		if tx.Journal.State == transaction.StateRolledBack {
			return nil, fmt.Errorf("transaction %s was already rolled back", id)
		}
		return tx, nil
	}
	journals, err := transaction.List(stateRoot)
	if err != nil {
		return nil, err
	}
	for _, journal := range journals {
		if journal.State == transaction.StateRolledBack || !strings.HasPrefix(journal.Description, "install ") {
			continue
		}
		return transaction.Open(stateRoot, journal.ID)
	}
	return nil, errors.New("no installation transaction is available for rollback")
}
