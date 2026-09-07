"""Entry point: `python -m client_app` launches the ttkbootstrap GUI.

Run from firmware/host/checkpoint_client (this folder must be the
working directory or on sys.path). Use `--cli` for the headless client.
"""

import sys


def main() -> None:
    if "--cli" in sys.argv:
        from .ble_client import cli_main
        cli_main()
    else:
        from .ui_main import main as gui_main
        gui_main()


if __name__ == "__main__":
    main()
