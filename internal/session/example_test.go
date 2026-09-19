package session_test

import (
	"context"
	"fmt"
	"os"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/session"
)

func ExampleManager() {
	m, _ := session.NewManager(os.TempDir(), observability.LoggingTracer{}, session.ManagerOptions{})
	id, _ := m.Start(context.Background(), session.Config{
		Command: []string{"sh", "-c", "printf 'hello session\\n'"},
		Query:   `log{ log:body = "hello session" } | matches`,
	})
	result, _ := m.Wait(context.Background(), id, 0, 10)
	count := len(result.Records)
	for !result.State.Terminal() {
		result, _ = m.Wait(context.Background(), id, result.NextCursor, 10)
		count += len(result.Records)
	}
	fmt.Println(count > 0, result.State)
	_ = m.Close()
	// Output: true exited
}
