# Task 2 Report

## Created

- `gui/go.mod`
- `gui/wails.json`
- `gui/frontend/dist/index.html`
- `gui/.gitignore`

## Verification

- Parsed `gui/wails.json` with `python3 json.loads(...)`
- Parsed `gui/frontend/dist/index.html` with Python's `html.parser.HTMLParser`
- Confirmed both checks succeeded: `JSON_OK`, `HTML_OK`

## Commit(s)

- `727254c5a883108b6a812b0fbbb6c1ebac25c55c`

## Concerns / Deviations

- Used Wails `v2.15.0` instead of the brief's example `v2.10.1`, because the current latest Wails v2 release is newer and still on the v2 major line.
