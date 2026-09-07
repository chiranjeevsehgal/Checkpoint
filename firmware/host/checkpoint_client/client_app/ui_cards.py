"""ttkbootstrap widget builders. Layout only; behavior lives in ui_main."""

import tkinter as tk

import ttkbootstrap as ttk
from ttkbootstrap.constants import HORIZONTAL, LEFT, RIGHT, X, BOTH, Y


def build_connection_bar(parent, device_var, on_connect, on_disconnect):
    top = ttk.Frame(parent, padding=8)
    top.pack(fill=X)
    ttk.Label(top, text="Device:").pack(side=LEFT)
    ttk.Entry(top, textvariable=device_var, width=22).pack(side=LEFT, padx=4)
    btn_connect = ttk.Button(top, text="Connect", command=on_connect, bootstyle="success")
    btn_connect.pack(side=LEFT, padx=4)
    btn_disc = ttk.Button(top, text="Disconnect", command=on_disconnect,
                          bootstyle="secondary", state="disabled")
    btn_disc.pack(side=LEFT)
    dot = tk.Label(top, text="\u25cf", fg="grey", font=("TkDefaultFont", 14))
    dot.pack(side=LEFT, padx=8)
    status_var = tk.StringVar(value="idle")
    ttk.Label(top, textvariable=status_var).pack(side=LEFT)
    return btn_connect, btn_disc, dot, status_var


def build_options_card(parent, ingest_var, vad_var, bench_var, keep_var,
                       thr_var, min_var):
    opts = ttk.Labelframe(parent, text="Options", padding=6, bootstyle="primary")
    opts.pack(fill=X, padx=8)
    ttk.Checkbutton(opts, text="Ingest", variable=ingest_var,
                    bootstyle="primary-round-toggle").pack(side=LEFT, padx=4)
    ttk.Checkbutton(opts, text="VAD", variable=vad_var,
                    bootstyle="primary-round-toggle").pack(side=LEFT)
    ttk.Checkbutton(opts, text="Bench CSV", variable=bench_var,
                    bootstyle="primary-round-toggle").pack(side=LEFT, padx=4)
    ttk.Checkbutton(opts, text="Keep files", variable=keep_var,
                    bootstyle="primary-round-toggle").pack(side=LEFT)
    ttk.Label(opts, text="Thr:").pack(side=LEFT, padx=(8, 0))
    ttk.Entry(opts, textvariable=thr_var, width=6).pack(side=LEFT)
    ttk.Label(opts, text="Min(s):").pack(side=LEFT, padx=(8, 0))
    ttk.Entry(opts, textvariable=min_var, width=6).pack(side=LEFT)
    return opts


def build_device_card(parent, actions, dev_status_var, led_muted_var,
                       bright_var, sync_var):
    dev = ttk.Labelframe(parent, text="Device (BLE remote)", padding=6, bootstyle="primary")
    dev.pack(fill=X, padx=8, pady=(4, 0))
    btn_rec = ttk.Button(dev, text="Rec …", command=actions["rec_toggle"],
                         bootstyle="secondary", state="disabled")
    btn_rec.pack(side=LEFT, padx=2)
    btn_status = ttk.Button(dev, text="Refresh", command=actions["status"],
                            bootstyle="info", state="disabled")
    btn_status.pack(side=LEFT, padx=2)
    ttk.Label(dev, textvariable=dev_status_var).pack(side=LEFT, padx=8)
    led_chk = ttk.Checkbutton(dev, text="LED muted", variable=led_muted_var,
                              command=actions["led_toggle"],
                              bootstyle="warning-round-toggle", state="disabled")
    led_chk.pack(side=LEFT, padx=4)
    ttk.Label(dev, text="Bright:").pack(side=LEFT, padx=(8, 0))
    bright_scale = ttk.Scale(dev, from_=5, to=255, variable=bright_var,
                             command=actions["bright_slide"],
                             orient=HORIZONTAL, length=110, state="disabled")
    bright_scale.pack(side=LEFT, padx=2)
    bright_scale.bind("<ButtonRelease-1>", actions["bright_release"])
    sync_chk = ttk.Checkbutton(dev, text="Auto-sync", variable=sync_var,
                               command=actions["sync_toggle"],
                               bootstyle="primary-round-toggle", state="disabled")
    sync_chk.pack(side=LEFT, padx=4)
    return {"rec": btn_rec, "status": btn_status,
            "led_chk": led_chk, "bright": bright_scale, "sync_chk": sync_chk}


def build_storage_card(parent, actions, storage_var, list_page_var):
    stor = ttk.Labelframe(parent, text="Storage (BLE remote)", padding=6, bootstyle="primary")
    stor.pack(fill=X, padx=8, pady=(4, 0))
    btn_storage = ttk.Button(stor, text="Refresh", command=actions["refresh"],
                             bootstyle="info", state="disabled")
    btn_storage.pack(side=LEFT, padx=2)
    ttk.Label(stor, textvariable=storage_var).pack(side=LEFT, padx=8)
    bar = ttk.Progressbar(stor, mode="determinate", maximum=100, length=140,
                          bootstyle="success-striped")
    bar.pack(side=LEFT, padx=2)
    bar["value"] = 0
    btn_delete = ttk.Button(stor, text="Delete", command=actions["delete"],
                            bootstyle="danger-outline", state="disabled")
    btn_delete.pack(side=RIGHT, padx=2)
    btn_erase = ttk.Button(stor, text="Erase all…", command=actions["erase"],
                           bootstyle="danger", state="disabled")
    btn_erase.pack(side=RIGHT, padx=2)
    btn_next = ttk.Button(stor, text="Next >", command=actions["next"],
                          bootstyle="secondary-outline", state="disabled")
    btn_next.pack(side=RIGHT, padx=2)
    btn_prev = ttk.Button(stor, text="< Prev", command=actions["prev"],
                          bootstyle="secondary-outline", state="disabled")
    btn_prev.pack(side=RIGHT, padx=2)
    ttk.Label(stor, textvariable=list_page_var).pack(side=RIGHT, padx=4)
    dev_tree = ttk.Treeview(stor, columns=("size", "state"), height=4,
                            show="tree headings", bootstyle="primary")
    dev_tree.heading("#0", text="Device file")
    dev_tree.heading("size", text="Size")
    dev_tree.heading("state", text="State")
    dev_tree.column("#0", width=260)
    dev_tree.column("size", width=90)
    dev_tree.column("state", width=140)
    dev_tree.pack(fill=X, pady=(4, 0))
    return {"refresh": btn_storage, "delete": btn_delete, "erase": btn_erase,
            "next": btn_next, "prev": btn_prev, "bar": bar, "dev_tree": dev_tree}


def build_transfers_card(parent):
    files = ttk.Labelframe(parent, text="Audio items", padding=6, bootstyle="primary")
    files.pack(fill=X, padx=8, pady=(4, 0))
    tree = ttk.Treeview(files, columns=("size", "ble", "vad", "ingest"), height=7,
                        show="tree headings", bootstyle="primary")
    tree.heading("#0", text="File")
    tree.heading("size", text="Size")
    tree.heading("ble", text="BLE")
    tree.heading("vad", text="VAD")
    tree.heading("ingest", text="Ingest")
    tree.column("#0", width=180)
    tree.column("size", width=80)
    tree.column("ble", width=110)
    tree.column("vad", width=130)
    tree.column("ingest", width=150)
    tree.pack(fill=X)
    prow = ttk.Frame(files)
    prow.pack(fill=X, pady=(4, 0))
    prog_label = ttk.Label(prow, text="active: —")
    prog_label.pack(side=LEFT)
    prog = ttk.Progressbar(prow, mode="determinate", maximum=100,
                           bootstyle="success-striped")
    prog.pack(side=LEFT, fill=X, expand=True, padx=8)
    return tree, prog, prog_label


def build_log_card(parent):
    logs = ttk.Labelframe(parent, text="Logs", padding=6, bootstyle="primary")
    logs.pack(fill=BOTH, expand=True, padx=8, pady=4)
    text = tk.Text(logs, height=12, wrap="none", state="disabled")
    sb = ttk.Scrollbar(logs, command=text.yview, bootstyle="primary-round")
    text.configure(yscrollcommand=sb.set)
    text.pack(side=LEFT, fill=BOTH, expand=True)
    sb.pack(side=RIGHT, fill=Y)
    return text
