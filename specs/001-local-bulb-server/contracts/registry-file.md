# Contract: Registry File

**Feature**: 001-local-bulb-server | Spec: FR-004

Location: `$XDG_CONFIG_HOME/haigosmart/registry.json` (`os.UserConfigDir()`), overridable
with `-registry`.

Written atomically: temp file in the same directory → `fsync` → `os.Rename`. A crash
mid-write therefore leaves the previous good file intact, never a truncated one.

```json
{
  "version": 1,
  "bulbs": [
    {
      "device_id": "a41f2c9b0e",
      "name": "kitchen",
      "first_seen": "2026-08-27T14:02:11Z",
      "last_seen": "2026-08-27T18:41:03Z",
      "capabilities": {
        "color": true,
        "color_temp": true,
        "min_brightness": 1
      },
      "state": {
        "power": true,
        "brightness": 80,
        "color": {"r": 255, "g": 190, "b": 120},
        "color_temp_k": 2700,
        "mode": "white",
        "reported_at": "2026-08-27T18:41:03Z"
      }
    }
  ]
}
```

Rules:

- `version` is checked on load. An unknown version is a startup error naming the file and
  the expected version — never a silent overwrite of the operator's data.
- A missing file is not an error: it means a first run, and an empty registry is created.
- A corrupt file is a startup error, not a silent reset. The file is left untouched so the
  operator can inspect or restore it.
- `device_id` is unique within the file; duplicates on load are an error.
- `name` defaults to `device_id` and is unique within the file.
- Connection status is deliberately absent — everything starts `Disconnected` on load and
  is proven by an actual connection (data-model.md).
- Timestamps are RFC 3339 UTC.
