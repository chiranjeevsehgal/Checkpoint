"""Entry point: `python -m bootstrap_app` launches the ttkbootstrap window."""


def main() -> None:
    from .ui_main import main as gui_main

    gui_main()


if __name__ == '__main__':
    main()
