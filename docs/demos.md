# Demos

AegisKeys uses [VHS](https://github.com/charmbracelet/vhs) tapes for
reproducible terminal demos. The tapes live in `demos/vhs/` and write generated
media to `docs/demo/`.

Run all demos:

```bash
make demo
```

Run a single demo:

```bash
make demo-cli
make demo-tui
make demo-full
```

The tapes use throwaway config directories under `tmp/` and fake demo
passwords. They do not use real API keys and should not be edited to include
real secrets.

Current tapes:

- `demos/vhs/cli-overview.tape` — initializes a demo vault, lists providers,
  verifies the Aider adapter, and runs `doctor`.
- `demos/vhs/tui-matrix-logo.tape` — opens the TUI and records the matrix-rain
  logo reveal across core screens.
- `demos/vhs/full-flow-launch.tape` — records a slower end-to-end product demo:
  demo vault setup, provider/model catalog, masked keys, profiles, matrix logo
  reveals, safe child-process launch, doctor, audit, settings, scratchpad note
  creation/selection/copy, and help.
