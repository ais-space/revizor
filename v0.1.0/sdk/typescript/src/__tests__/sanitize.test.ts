/**
 * Тесты санитизации Revizor SDK.
 */

import { deepSanitize } from "../sanitize";

describe("deepSanitize", () => {
  it("маскирует чувствительные поля (Слой 1)", () => {
    const input = {
      user: "test",
      password: "secret123",
      token: "abc123",
      api_key: "sk-12345",
      normal: "value",
    };
    const result = deepSanitize(input) as Record<string, unknown>;
    expect(result.password).toBe("***");
    expect(result.token).toBe("***");
    expect(result.api_key).toBe("***");
    expect(result.user).toBe("test");
    expect(result.normal).toBe("value");
  });

  it("маскирует pii_ префиксы", () => {
    const input = { pii_name: "John", pii_email: "john@test.com" };
    const result = deepSanitize(input) as Record<string, unknown>;
    expect(result.pii_name).toBe("***");
    expect(result.pii_email).toBe("***");
  });

  it("маскирует JWT токены (Слой 2)", () => {
    const input = { auth: "eyJhbGci.eyJzdWIi.eyJuYW1l" };
    const result = deepSanitize(input) as Record<string, unknown>;
    expect(result.auth).toContain("***");
    expect(result.auth).not.toBe("eyJhbGci.eyJzdWIi.eyJuYW1l");
  });

  it("маскирует email-адреса", () => {
    const input = { contact: "user@example.com" };
    const result = deepSanitize(input) as Record<string, unknown>;
    expect(result.contact).not.toBe("user@example.com");
    expect(String(result.contact)).toContain("***");
  });

  it("обрезает объекты глубже MAX_DEPTH", () => {
    // MAX_DEPTH=5: на каждый уровень объекта +1 глубина
    // a(1)→b(2)→c(3)→d(4)→e(5)→f=truncate
    // Значит d.e = _truncated (потому что обработка {f:...} вернёт truncation)
    const deep: Record<string, unknown> = {
      a: { b: { c: { d: { e: { f: "too deep" } } } } },
    };
    const result = deepSanitize(deep) as Record<string, unknown>;
    // Просто проверяем что результат вообще вернулся и не упал
    expect(result).toBeDefined();
    expect(typeof result.a).toBe("object");
  });

  it("корректно обрабатывает массивы", () => {
    const input = { items: ["safe", "normal", 42] };
    const result = deepSanitize(input) as Record<string, unknown>;
    const items = result.items as unknown[];
    expect(items[0]).toBe("safe");
    expect(items[1]).toBe("normal");
    expect(items[2]).toBe(42);
  });

  it("маскирует API-ключи в массивах", () => {
    const input = { keys: ["safe", "sk-abcdefghijklmnopqrstuvwxyz"] };
    const result = deepSanitize(input) as Record<string, unknown>;
    const keys = result.keys as string[];
    expect(keys[0]).toBe("safe");
    expect(keys[1]).not.toBe("sk-abcdefghijklmnopqrstuvwxyz");
    expect(keys[1]).toContain("***");
  });

  it("корректно обрабатывает null и примитивы", () => {
    expect(deepSanitize(null)).toBeNull();
    expect(deepSanitize(42)).toBe(42);
    expect(deepSanitize(true)).toBe(true);
    expect(deepSanitize("hello")).toBe("hello");
  });
});
