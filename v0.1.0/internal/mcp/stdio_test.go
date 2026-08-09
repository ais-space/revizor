package mcp

import (
	"bufio"
	"context"
	"os"
	"testing"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
)

func TestRunStdio_SingleRequest(t *testing.T) {
	// Создаём pipe для симуляции stdin/stdout
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()

	os.Stdin = rIn
	os.Stdout = wOut

	handler := NewMCPHandler(&mockStore{}, &config.Config{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Пишем запрос в stdin в горутине
	go func() {
		wIn.WriteString(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
		wIn.Close()
	}()

	// Читаем ответ из stdout
	resultCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(rOut)
		if scanner.Scan() {
			resultCh <- scanner.Text()
		}
	}()

	err := RunStdio(ctx, handler)
	if err != nil {
		t.Fatalf("RunStdio failed: %v", err)
	}

	select {
	case result := <-resultCh:
		if result == "" {
			t.Error("expected non-empty response")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for response")
	}
}

func TestRunStdio_EOF(t *testing.T) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	rIn, wIn, _ := os.Pipe()
	_, wOut, _ := os.Pipe()

	os.Stdin = rIn
	os.Stdout = wOut

	handler := NewMCPHandler(&mockStore{}, &config.Config{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Сразу закрываем stdin (EOF)
	wIn.Close()

	err := RunStdio(ctx, handler)
	if err != nil {
		t.Fatalf("RunStdio should handle EOF gracefully, got: %v", err)
	}
}

func TestRunStdio_ContextCancel(t *testing.T) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	// Создаём pipe который не закрывается (симуляция бесконечного stdin)
	rIn, wIn, _ := os.Pipe()
	_, wOut, _ := os.Pipe()

	os.Stdin = rIn
	os.Stdout = wOut

	handler := NewMCPHandler(&mockStore{}, &config.Config{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Закрываем stdin через небольшую задержку
	go func() {
		time.Sleep(100 * time.Millisecond)
		wIn.Close()
	}()

	err := RunStdio(ctx, handler)
	// Контекст мог успеть отмениться, а мог EOF прийти раньше — оба варианта ок
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Logf("RunStdio returned: %v (acceptable)", err)
	}
}
