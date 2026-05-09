# Ledger README

This ledger is a non-authoritative recovery surface.

## Files

- `tracking-ledger.md`: object-oriented status table.
- `execution/`: round-oriented execution entries.
- `templates/`: entry templates.

## Rules

- Ledger can record stop points and references.
- Ledger cannot decide acceptance.
- Ledger cannot mark `passes:true`.
- Ledger cannot Close.
- Ledger cannot replace `longterm` or feature evidence.

## Minimum Reading Order

1. `../14-P0-Temporary-Ledger-Mechanism.md`
2. `tracking-ledger.md`
3. latest file under `execution/`
