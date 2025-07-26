# Release v2.0.1

## 🐛 Bug Fix

### Fixed Module Path for Go Modules v2

- Updated `go.mod` to use `github.com/semihalev/zlog/v2` module path
- Updated all import statements in examples and documentation
- This fix ensures proper Go modules v2 compatibility

## 📦 Installation

```bash
go get github.com/semihalev/zlog/v2
```

## 🔄 Migration from v2.0.0

If you already installed v2.0.0, update your imports:

```go
// Old (incorrect)
import "github.com/semihalev/zlog"

// New (correct) 
import "github.com/semihalev/zlog/v2"
```

---

For full v2 features and changes, see the [v2.0.0 release notes](https://github.com/semihalev/zlog/releases/tag/v2.0.0).