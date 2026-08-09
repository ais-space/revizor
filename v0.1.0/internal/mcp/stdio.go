package mcp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// RunStdio запускает MCP-сервер в режиме stdio:
// читает JSON-RPC запросы из os.Stdin, пишет ответы в os.Stdout.
// Логи идут в os.Stderr (чтобы не нарушать протокол).
func RunStdio(ctx context.Context, handler *MCPHandler) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("MCP stdio mode started")

	scanner := bufio.NewScanner(os.Stdin)
	// Увеличиваем буфер для больших JSON-RPC запросов
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		// Проверка отмены контекста
		select {
		case <-ctx.Done():
			logger.Info("MCP stdio shutting down (context cancelled)")
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		response := handler.HandleJSONRPCSafe(ctx, line)
		if response == nil {
			// Уведомление (нет id) — ответ не требуется
			continue
		}

		// Записываем ответ в stdout
		if _, err := fmt.Fprintln(os.Stdout, string(response)); err != nil {
			logger.Error("failed to write response to stdout", "error", err)
			return fmt.Errorf("stdout write: %w", err)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		logger.Error("stdin read error", "error", err)
		return fmt.Errorf("stdin read: %w", err)
	}

	logger.Info("MCP stdio mode stopped (EOF)")
	return nil
}
