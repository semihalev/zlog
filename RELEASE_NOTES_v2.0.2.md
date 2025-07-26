# Release v2.0.2

## 🎯 Better Out-of-Box Experience

### Default Logger Now Uses Terminal Output

The global default logger now automatically uses colored terminal output instead of binary format. This provides a much better user experience, especially for new users and during development.

## 🐛 Bug Fixes

- **Fixed early logging showing binary format** - Logs that happen before logger configuration (like "Register middleware") now display properly
- **Default logger initialization** - Changed from binary stderr to terminal stdout for human-readable output

## 💡 What Changed

Before v2.0.2:
```go
// Early logs would show as: GOLZ...binary data...
zlog.Info("Starting application")  // Binary format by default
```

After v2.0.2:
```go
// Now shows as: INFO [01-25|20:45:00] Starting application
zlog.Info("Starting application")  // Beautiful colored output by default
```

## 🔧 Usage

No code changes required! The default logger now "just works" with terminal output.

You can still customize as before:
```go
// Use binary format if needed
zlog.SetWriter(os.Stderr)  

// Or use your own configuration
logger := zlog.NewStructured()
logger.SetWriter(zlog.StderrTerminal())
zlog.SetDefault(logger)
```

## 📦 Installation

```bash
go get github.com/semihalev/zlog/v2@v2.0.2
```

---

This release improves the developer experience by making zlog work beautifully out of the box without any configuration.