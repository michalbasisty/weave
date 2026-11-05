# Capability: orders

Status: fixture (2026-01-01)

The warehouse sees every order on one screen: the page calls the orders API, and the
API reads and writes the orders table. This fixture exists so the panel smoke test has
a screen, an endpoint and an owning spec to cross-reference.

## Parts

| Path | What it does |
| ---- | ------------ |
| `packages/backend/src/modules/orders/orders.routes.ts` | The orders API: list and create. |
| `packages/web/src/pages/Orders.tsx` | The orders screen. |

## How to use this table

This is the map, not the contract. Behaviour lives in the code and its tests.
