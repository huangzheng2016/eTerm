(function () {
  "use strict";

  // ---------- utils ----------

  function lsGet(k) {
    try { return localStorage.getItem(k); } catch (e) { return null; }
  }
  function lsSet(k, v) {
    try { localStorage.setItem(k, v); } catch (e) {}
  }
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function pad(n) {
    return String(n).padStart(2, "0");
  }
  function wsURL(path) {
    return (location.protocol === "https:" ? "wss://" : "ws://") + location.host + path;
  }
  var encoder = new TextEncoder();
  var decoder = new TextDecoder();
  function b64encode(str) {
    var bytes = encoder.encode(str);
    var bin = "";
    for (var i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return btoa(bin);
  }
  function b64decode(b64) {
    var bin = atob(b64);
    var bytes = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return bytes;
  }
  // Send large pastes in chunks so a single frame stays well under the
  // server's 1MiB read limit: 64K chars is at most 256KB of UTF-8.
  function chunkSend(send, str) {
    var i = 0;
    while (i < str.length) {
      var end = Math.min(i + 65536, str.length);
      if (end < str.length) {
        var c = str.charCodeAt(end - 1);
        if (c >= 0xd800 && c <= 0xdbff) end--;
      }
      send(str.substring(i, end));
      i = end;
    }
  }

  // ---------- relay frame codec ----------
  // 10-byte header [type:1][flags:1][streamID:4][payloadLen:4] big-endian,
  // one frame per binary WS message (internal/relay/frame.go).

  var F_OPEN = 0x10, F_OPEN_OK = 0x11, F_OPEN_ERR = 0x12,
      F_DATA = 0x20, F_RESIZE = 0x21, F_CLOSE = 0x22, F_ACK = 0x23,
      F_HELLO_ERR = 0x04;
  var HDR = 10;

  function encFrame(type, streamID, payload) {
    var buf = new ArrayBuffer(HDR + payload.length);
    var dv = new DataView(buf);
    dv.setUint8(0, type);
    dv.setUint8(1, 0);
    dv.setUint32(2, streamID);
    dv.setUint32(6, payload.length);
    new Uint8Array(buf, HDR).set(payload);
    return buf;
  }
  function decFrame(bytes) {
    var dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    return {
      type: dv.getUint8(0),
      streamID: dv.getUint32(2),
      payload: bytes.subarray(HDR)
    };
  }
  function jsonBytes(o) {
    return encoder.encode(JSON.stringify(o));
  }
  function newStreamID() {
    var v = crypto.getRandomValues(new Uint32Array(1))[0];
    return v === 0 ? 1 : v;
  }

  // ---------- share transport (JSON text frames, /x/{token}/ws) ----------

  function ShareTransport(token, expiresAt) {
    this.token = token;
    this.expiresAt = expiresAt;
    this.alive = true;
    this.retries = 0;
    this.ws = null;
    this.h = null;
    this.reconnectTimer = null;
  }
  ShareTransport.prototype.start = function (h, rows, cols) {
    this.h = h;
    this.rows = rows;
    this.cols = cols;
    this.connect();
  };
  ShareTransport.prototype.send = function (o) {
    if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify(o));
  };
  ShareTransport.prototype.sendInput = function (str) {
    if (this.alive) this.send({ t: "in", d: b64encode(str) });
  };
  ShareTransport.prototype.sendInputChunked = function (str) {
    var self = this;
    chunkSend(function (s) { self.sendInput(s); }, str);
  };
  ShareTransport.prototype.sendResize = function (rows, cols) {
    if (this.alive) this.send({ t: "resize", rows: rows, cols: cols });
  };
  ShareTransport.prototype.close = function () {
    this.alive = false;
    clearTimeout(this.reconnectTimer);
    if (this.ws) this.ws.close();
  };
  ShareTransport.prototype.connect = function () {
    var self = this;
    this.ws = new WebSocket(wsURL("/x/" + this.token + "/ws"));
    this.ws.onopen = function () {
      self.retries = 0;
      self.h.onstatus("connected");
      self.send({ t: "resize", rows: self.rows, cols: self.cols });
    };
    this.ws.onmessage = function (ev) {
      var m = JSON.parse(ev.data);
      if (m.t === "out") {
        self.h.onoutput(b64decode(m.d));
      } else if (m.t === "exit") {
        self.alive = false;
        self.h.onexit(m.reason || "session closed");
      }
    };
    this.ws.onclose = function () {
      if (!self.alive) return;
      if (Date.now() >= self.expiresAt.getTime()) {
        self.alive = false;
        self.h.onstatus("disconnected");
        return;
      }
      // A deleted share makes the handshake fail with 404; the WebSocket API
      // hides the status, so probe the page URL before retrying.
      fetch(location.pathname, { method: "HEAD", cache: "no-store" }).then(function (r) {
        if (r.status === 404) {
          self.alive = false;
          self.h.onstatus("disconnected: share deleted");
        } else {
          self.scheduleReconnect();
        }
      }).catch(function () {
        self.scheduleReconnect();
      });
    };
  };
  ShareTransport.prototype.scheduleReconnect = function () {
    var self = this;
    if (!this.alive) return;
    if (this.retries >= 12) {
      this.alive = false;
      this.h.onexit("reconnect limit reached");
      return;
    }
    var delay = Math.min(10000, 1000 * Math.pow(2, this.retries));
    this.retries++;
    this.h.onstatus("reconnecting...");
    this.reconnectTimer = setTimeout(function () { self.connect(); }, delay);
  };

  // ---------- relay session (binary frames, /api/v1/ws/client) ----------

  function RelaySession(opts) {
    this.o = opts;
    this.streamID = newStreamID();
    this.acked = 0;
    this.lastAck = 0;
    this.opened = false;
    this.degraded = false;
    this.closed = false;
    this.retries = 0;
    this.ws = null;
    this.h = null;
    this.rows = 24;
    this.cols = 80;
    this.reconnectTimer = null;
  }
  RelaySession.prototype.start = function (h, rows, cols) {
    this.h = h;
    this.rows = rows;
    this.cols = cols;
    this.connect();
  };
  RelaySession.prototype.openPayload = function (resume) {
    var o = this.o;
    return jsonBytes({
      peer_id: o.peerID,
      target: o.target,
      host_sync_id: o.hostSyncID || undefined,
      session_id: o.sessionID || undefined,
      name: o.name || undefined,
      rows: this.rows,
      cols: this.cols,
      resume_from_seq: resume || undefined
    });
  };
  RelaySession.prototype.sendFrame = function (type, payload) {
    if (this.ws && this.ws.readyState === 1) {
      this.ws.send(encFrame(type, this.streamID, payload));
    }
  };
  RelaySession.prototype.sendOpen = function (resume) {
    this.sendFrame(F_OPEN, this.openPayload(resume));
  };
  RelaySession.prototype.connect = function () {
    var self = this;
    this.h.onstatus(this.acked > 0 || this.retries > 0 ? "reconnecting..." : "connecting");
    this.ws = new WebSocket(wsURL("/api/v1/ws/client"));
    this.ws.binaryType = "arraybuffer";
    this.ws.onopen = function () {
      self.sendOpen(self.acked);
    };
    this.ws.onmessage = function (ev) {
      self.onFrame(decFrame(new Uint8Array(ev.data)));
    };
    this.ws.onclose = function () {
      if (self.closed || self.fatal) return;
      self.opened = false;
      self.scheduleReconnect();
    };
  };
  RelaySession.prototype.onFrame = function (f) {
    if (f.streamID !== this.streamID) return;
    var h = this.h;
    if (f.type === F_OPEN_OK) {
      this.opened = true;
      this.retries = 0;
      h.onstatus("connected");
      if (this.pendingResize) {
        var pr = this.pendingResize;
        this.pendingResize = null;
        this.sendResize(pr[0], pr[1]);
      }
    } else if (f.type === F_OPEN_ERR || f.type === F_HELLO_ERR) {
      var text = decoder.decode(f.payload);
      var info = null;
      try { info = JSON.parse(text); } catch (e) {}
      if (info && info.code === "fingerprint_unconfirmed") {
        h.onstatus("waiting: host key confirmation");
        h.onfingerprint(info);
        return;
      }
      if (text === "resume unavailable" && !this.degraded) {
        // The daemon lost the stream (restart). Reopen as a fresh session:
        // tmux attaches by session_id again; local/host start a new shell.
        this.degraded = true;
        this.streamID = newStreamID();
        this.acked = 0;
        this.lastAck = 0;
        h.onstatus("session lost; starting a new one...");
        this.sendOpen(0);
        return;
      }
      this.fatal = true;
      h.onexit(text || "open failed");
    } else if (f.type === F_DATA) {
      var dv = new DataView(f.payload.buffer, f.payload.byteOffset, f.payload.byteLength);
      var seq = Number(dv.getBigUint64(0));
      var data = f.payload.subarray(8);
      if (seq + data.length <= this.acked) return; // duplicate from resume
      if (seq > this.acked && this.acked > 0) {
        // Gap after resume: rebase to the first frame the daemon has left.
        this.acked = seq;
        this.lastAck = seq;
      }
      this.acked = seq + data.length;
      h.onoutput(data);
      if (this.acked - this.lastAck >= 256 * 1024) {
        this.lastAck = this.acked;
        var ack = new ArrayBuffer(8);
        new DataView(ack).setBigUint64(0, BigInt(this.acked));
        this.sendFrame(F_ACK, new Uint8Array(ack));
      }
    } else if (f.type === F_CLOSE) {
      this.fatal = true;
      h.onexit(decoder.decode(f.payload) || "session closed");
    }
  };
  RelaySession.prototype.scheduleReconnect = function () {
    var self = this;
    if (this.closed) return;
    if (this.retries >= 12) {
      this.fatal = true;
      this.h.onexit("reconnect limit reached");
      return;
    }
    var delay = Math.min(10000, 1000 * Math.pow(2, this.retries));
    this.retries++;
    this.h.onstatus("reconnecting...");
    this.reconnectTimer = setTimeout(function () { self.connect(); }, delay);
  };
  RelaySession.prototype.sendInput = function (str) {
    if (this.opened) this.sendFrame(F_DATA, encoder.encode(str));
  };
  RelaySession.prototype.sendInputChunked = function (str) {
    var self = this;
    chunkSend(function (s) { self.sendInput(s); }, str);
  };
  RelaySession.prototype.sendResize = function (rows, cols) {
    rows = Math.max(1, Math.min(rows, 65535));
    cols = Math.max(1, Math.min(cols, 65535));
    if (!this.opened) {
      // Rotation while disconnected: apply once the resumed session is open.
      this.pendingResize = [rows, cols];
      return;
    }
    var buf = new ArrayBuffer(4);
    var dv = new DataView(buf);
    dv.setUint16(0, rows);
    dv.setUint16(2, cols);
    this.sendFrame(F_RESIZE, new Uint8Array(buf));
  };
  RelaySession.prototype.confirmFingerprint = function (info) {
    var payload = jsonBytes({
      peer_id: this.o.peerID,
      target: "host-fingerprint-accept",
      host_sync_id: info.host_sync_id,
      fingerprint: info.fingerprint,
      alg: info.alg,
      rows: this.rows,
      cols: this.cols
    });
    this.sendFrame(F_OPEN, payload);
  };
  RelaySession.prototype.close = function () {
    this.closed = true;
    clearTimeout(this.reconnectTimer);
    if (this.ws) this.ws.close();
  };

  // One-shot tmux-list fetch on its own short-lived connection.
  function fetchTmuxList(peerID, cb) {
    var ws, streamID = newStreamID(), done = false;
    var timer = setTimeout(function () { finish(new Error("timeout")); }, 10000);
    function finish(err, list) {
      if (done) return;
      done = true;
      clearTimeout(timer);
      try { ws.close(); } catch (e) {}
      cb(err, list);
    }
    ws = new WebSocket(wsURL("/api/v1/ws/client"));
    ws.binaryType = "arraybuffer";
    ws.onopen = function () {
      ws.send(encFrame(F_OPEN, streamID, jsonBytes({ peer_id: peerID, target: "tmux-list" })));
    };
    ws.onmessage = function (ev) {
      var f = decFrame(new Uint8Array(ev.data));
      if (f.streamID !== streamID) return;
      if (f.type === F_OPEN_OK) {
        var list = [];
        try { list = JSON.parse(decoder.decode(f.payload)); } catch (e) {}
        finish(null, list);
      } else if (f.type === F_OPEN_ERR) {
        finish(new Error(decoder.decode(f.payload) || "tmux-list failed"));
      } else if (f.type === F_CLOSE) {
        finish(null, []);
      }
    };
    ws.onclose = function () {
      if (!done) finish(new Error("connection closed"));
    };
    ws.onerror = function () {};
  }

  // ---------- terminal screen (xterm + mobile chrome, shared by both views) ----------

  function TermScreen(root, opts) {
    this.opts = opts;
    this.transport = opts.transport;
    this.bracketed = false;
    this.destroyed = false;

    var screen = el("div", "tscreen");
    var bar = el("div", "bar");
    if (opts.onBack) {
      var backBtn = el("button", "btn", "‹ Back");
      backBtn.addEventListener("click", opts.onBack);
      bar.appendChild(backBtn);
    }
    this.nameEl = el("span", "name", opts.title || "terminal");
    this.statusEl = el("span", "meta", "connecting");
    bar.appendChild(this.nameEl);
    bar.appendChild(this.statusEl);
    if (opts.expiresAt) {
      var expiresEl = el("span", "meta");
      var d = opts.expiresAt;
      var full = d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) +
        " " + pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds());
      expiresEl.textContent = "exp " + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) + " " +
        pad(d.getHours()) + ":" + pad(d.getMinutes());
      expiresEl.title = "expires " + full;
      bar.appendChild(expiresEl);
    }
    bar.appendChild(el("span", "spacer"));
    var btns = el("div", "btns");
    this.btnPaste = el("button", "btn", "Paste");
    this.btnKeys = el("button", "btn", "Keys");
    this.btnLock = el("button", "btn", "Lock");
    btns.appendChild(this.btnPaste);
    btns.appendChild(this.btnKeys);
    btns.appendChild(this.btnLock);
    bar.appendChild(btns);
    screen.appendChild(bar);

    this.termEl = el("div", "term");
    screen.appendChild(this.termEl);
    this.keysEl = el("div", "keysbar");
    screen.appendChild(this.keysEl);
    this.lockEl = el("div", "lockoverlay");
    screen.appendChild(this.lockEl);
    this.pasteBg = el("div", "pastebg");
    var pasteBox = el("div", "pastebox");
    this.pasteText = el("textarea");
    this.pasteText.placeholder = "paste text here";
    pasteBox.appendChild(this.pasteText);
    var pasteRow = el("div", "row");
    var pasteCancel = el("button", "btn", "Cancel");
    var pasteSend = el("button", "btn", "Send");
    pasteRow.appendChild(pasteCancel);
    pasteRow.appendChild(pasteSend);
    pasteBox.appendChild(pasteRow);
    this.pasteBg.appendChild(pasteBox);
    screen.appendChild(this.pasteBg);
    root.appendChild(screen);
    this.barEl = bar;

    var term = new Terminal({ cursorBlink: true });
    var fit = new FitAddon.FitAddon();
    term.loadAddon(fit);
    term.open(this.termEl);
    fit.fit();
    this.term = term;
    this.fit = fit;
    this.alive = true;

    var self = this;
    var transport = this.transport;
    transport.start({
      onstatus: function (s) { self.setStatus(s); },
      onoutput: function (bytes) { self.onOutput(bytes); },
      onexit: function (reason) {
        self.alive = false;
        self.setStatus("exited: " + reason);
      },
      onfingerprint: opts.onfingerprint
    }, term.rows, term.cols);

    // Ctrl/Alt latch: the next typed character is transformed once, then the
    // latch releases. Tapping the key again cancels.
    var ctrlLatch = false;
    var altLatch = false;
    var keyButtons = {};
    function updateKeyButtons() {
      keyButtons.Ctrl.classList.toggle("on", ctrlLatch);
      keyButtons.Alt.classList.toggle("on", altLatch);
    }
    function sendInput(str) {
      // Only transform single typed characters: a multi-character string is a
      // native paste arriving via onData and must pass through untouched.
      if ((ctrlLatch || altLatch) && str.length === 1) {
        if (ctrlLatch) {
          str = String.fromCharCode(str.charCodeAt(0) & 0x1f);
        } else {
          str = "\x1b" + str;
        }
        ctrlLatch = false;
        altLatch = false;
        updateKeyButtons();
      }
      transport.sendInput(str);
    }
    this.sendInput = sendInput;
    term.onData(function (d) {
      sendInput(d);
    });

    // Extra keys bar.
    var keyDefs = [
      { label: "Esc", data: "\x1b" },
      { label: "Tab", data: "\t" },
      { label: "Ctrl" },
      { label: "Alt" },
      { label: "←", data: "\x1b[D" },
      { label: "↑", data: "\x1b[A" },
      { label: "↓", data: "\x1b[B" },
      { label: "→", data: "\x1b[C" },
      { label: "Enter", data: "\r" },
      { label: "PgUp", scroll: -1 },
      { label: "PgDn", scroll: 1 },
      { label: "Top", scrollTop: true },
      { label: "Bottom", scrollBottom: true }
    ];
    keyDefs.forEach(function (def) {
      var b = el("button", "btn", def.label);
      // pointerdown + preventDefault keeps focus on xterm's hidden textarea,
      // so the soft keyboard stays open.
      b.addEventListener("pointerdown", function (e) {
        e.preventDefault();
        if (def.label === "Ctrl") {
          ctrlLatch = !ctrlLatch;
          if (ctrlLatch) altLatch = false;
          updateKeyButtons();
        } else if (def.label === "Alt") {
          altLatch = !altLatch;
          if (altLatch) ctrlLatch = false;
          updateKeyButtons();
        } else {
          if (def.scroll) term.scrollPages(def.scroll);
          else if (def.scrollTop) term.scrollToTop();
          else if (def.scrollBottom) term.scrollToBottom();
          else sendInput(def.data);
        }
      });
      keyButtons[def.label] = b;
      self.keysEl.appendChild(b);
    });

    var keysPref = lsGet("eterm-share-keys");
    var coarse = matchMedia("(pointer: coarse)").matches;
    if (keysPref === null) keysPref = coarse ? "1" : "0";
    function setKeys(v) {
      self.keysEl.style.display = v ? "flex" : "none";
      lsSet("eterm-share-keys", v ? "1" : "0");
      self.btnKeys.classList.toggle("on", v);
      // The keys bar is fixed at the bottom; shrink the terminal so the bar
      // never covers its last rows, then refit.
      self.termEl.style.marginBottom = v ? self.keysEl.offsetHeight + "px" : "0";
      fit.fit();
      transport.sendResize(term.rows, term.cols);
    }
    setKeys(keysPref === "1");
    this.btnKeys.addEventListener("click", function () {
      setKeys(self.keysEl.style.display !== "flex");
    });

    // Keep the keys bar on top of the soft keyboard.
    function placeKeys() {
      var vv = window.visualViewport;
      if (!vv) return;
      var bottom = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
      self.keysEl.style.bottom = bottom + "px";
    }
    this.placeKeys = placeKeys;

    // Soft keyboard must not resize the terminal: only a width change
    // (rotation, window management) triggers fit + resize.
    var lastWidth = window.visualViewport ? window.visualViewport.width : window.innerWidth;
    var resizeTimer;
    function scheduleFit() {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(function () {
        if (self.destroyed) return;
        fit.fit();
        transport.sendResize(term.rows, term.cols);
      }, 150);
    }
    function viewportChanged() {
      placeKeys();
      self.placeLock();
      var vv = window.visualViewport;
      var w = vv ? vv.width : window.innerWidth;
      if (coarse && Math.abs(w - lastWidth) < 1) return;
      lastWidth = w;
      scheduleFit();
    }
    this.viewportChanged = viewportChanged;
    window.addEventListener("resize", viewportChanged);
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", viewportChanged);
      window.visualViewport.addEventListener("scroll", placeKeys);
    }

    // Input lock: a transparent overlay swallows all pointer events so the
    // hidden textarea never gains focus and the soft keyboard never opens.
    var locked = lsGet("eterm-share-lock") === "1";
    // The overlay top tracks the bar height; it must be recomputed when the
    // bar wraps or fonts finish loading, not only when the lock toggles.
    this.placeLock = function () {
      this.lockEl.style.top = bar.offsetHeight + "px";
    };
    function setLock(v) {
      locked = v;
      lsSet("eterm-share-lock", v ? "1" : "0");
      self.placeLock();
      self.lockEl.style.display = v ? "block" : "none";
      self.btnLock.textContent = v ? "Locked" : "Lock";
      self.btnLock.classList.toggle("on", v);
      self.btnPaste.disabled = v;
      if (v && document.activeElement) document.activeElement.blur();
    }
    setLock(locked);
    this.isLocked = function () { return locked; };
    this.btnLock.addEventListener("click", function () {
      setLock(!locked);
    });
    this.lockEl.addEventListener("pointerdown", function (e) {
      e.preventDefault();
    });
    // Locked: the overlay swallows touches, so drags are forwarded to the
    // xterm viewport's own scroll position (pixel-level, clamped by the
    // browser); its scroll listener syncs the buffer.
    var viewportEl = this.termEl.querySelector(".xterm-viewport");
    var lockTouchY = null;
    this.lockEl.addEventListener("touchstart", function (e) {
      lockTouchY = e.touches[0].clientY;
      e.preventDefault();
    }, { passive: false });
    this.lockEl.addEventListener("touchmove", function (e) {
      var y = e.touches[0].clientY;
      viewportEl.scrollTop += lockTouchY - y;
      lockTouchY = y;
      e.preventDefault();
    }, { passive: false });
    this.lockEl.addEventListener("wheel", function (e) {
      term.scrollLines(e.deltaY > 0 ? 3 : -3);
      e.preventDefault();
    }, { passive: false });

    // Paste: batch input, wrapped in bracketed-paste markers when the remote
    // side enabled them.
    function openPaste() {
      if (locked) return;
      self.pasteBg.style.display = "flex";
      self.pasteText.focus();
    }
    function closePaste() {
      self.pasteBg.style.display = "none";
      self.pasteText.value = "";
      if (!locked) term.focus();
    }
    this.btnPaste.addEventListener("click", openPaste);
    pasteCancel.addEventListener("click", closePaste);
    this.pasteBg.addEventListener("click", function (e) {
      if (e.target === self.pasteBg) closePaste();
    });
    this.pasteText.addEventListener("keydown", function (e) {
      if (e.key === "Escape") closePaste();
    });
    pasteSend.addEventListener("click", function () {
      var text = self.pasteText.value;
      if (text) {
        if (self.bracketed) text = "\x1b[200~" + text + "\x1b[201~";
        transport.sendInputChunked(text);
      }
      closePaste();
    });
  }
  TermScreen.prototype.setStatus = function (s) {
    this.statusEl.textContent = s;
  };
  TermScreen.prototype.onOutput = function (bytes) {
    var s = "";
    for (var i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
    var h = s.lastIndexOf("\x1b[?2004h");
    var l = s.lastIndexOf("\x1b[?2004l");
    if (h > l) this.bracketed = true;
    else if (l > h) this.bracketed = false;
    this.term.write(bytes);
  };
  TermScreen.prototype.destroy = function () {
    this.destroyed = true;
    this.transport.close();
    window.removeEventListener("resize", this.viewportChanged);
    if (window.visualViewport) {
      window.visualViewport.removeEventListener("resize", this.viewportChanged);
      window.visualViewport.removeEventListener("scroll", this.placeKeys);
    }
    this.term.dispose();
    this.termEl.closest(".tscreen").remove();
  };

  // ---------- fingerprint dialog ----------

  var fpState = null;
  function showFingerprintDialog(root, info, onConfirm, onCancel) {
    hideFingerprintDialog();
    var bg = el("div", "fpb");
    var box = el("div", "box");
    box.appendChild(el("h2", null, "Unknown Host Key"));
    box.appendChild(el("div", "kv", "Host: " + (info.hostname || info.host_sync_id)));
    if (info.alias) box.appendChild(el("div", "kv", "Alias: " + info.alias));
    if (info.alg) box.appendChild(el("div", "kv", "Algorithm: " + info.alg));
    box.appendChild(el("div", "fp", info.fingerprint || ""));
    box.appendChild(el("div", "kv", "Verify this fingerprint with the server administrator before trusting it."));
    var row = el("div", "row");
    var cancel = el("button", "btn", "Cancel");
    var ok = el("button", "btn", "Trust and connect");
    row.appendChild(cancel);
    row.appendChild(ok);
    box.appendChild(row);
    bg.appendChild(box);
    root.appendChild(bg);
    bg.style.display = "flex";
    fpState = { bg: bg, ok: ok, cancel: cancel };
    ok.addEventListener("click", function () {
      hideFingerprintDialog();
      onConfirm();
    });
    cancel.addEventListener("click", function () {
      hideFingerprintDialog();
      onCancel();
    });
  }
  function hideFingerprintDialog() {
    if (fpState) {
      fpState.bg.remove();
      fpState = null;
    }
  }

  // ---------- login view ----------

  function renderLogin(root) {
    fetch("/api/v1/web/me", { cache: "no-store" }).then(function (r) {
      if (r.ok) location.href = "/app";
    }).catch(function () {});

    var wrap = el("div", "loginwrap");
    var card = el("div", "logincard");
    card.appendChild(el("h1", null, "eTerm"));
    var input = el("input");
    input.type = "password";
    input.placeholder = "password";
    input.autocomplete = "current-password";
    var btn = el("button", "btn", "Enter");
    var err = el("div", "err");
    card.appendChild(input);
    card.appendChild(btn);
    card.appendChild(err);
    wrap.appendChild(card);
    root.appendChild(wrap);

    function submit() {
      err.textContent = "";
      btn.disabled = true;
      fetch("/api/v1/web/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: input.value })
      }).then(function (r) {
        btn.disabled = false;
        if (r.ok) {
          location.href = "/app";
        } else if (r.status === 401) {
          err.textContent = "wrong password";
        } else {
          err.textContent = "login failed (HTTP " + r.status + ")";
        }
      }).catch(function () {
        btn.disabled = false;
        err.textContent = "login failed: network error";
      });
    }
    btn.addEventListener("click", submit);
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") submit();
    });
    input.focus();
  }

  // ---------- devices view ----------

  function renderDevices(root) {
    fetch("/api/v1/web/me", { cache: "no-store" }).then(function (r) {
      if (r.status === 401) {
        location.href = "/login";
        return null;
      }
      if (!r.ok) throw new Error("me failed");
      return r.json();
    }).then(function (me) {
      if (!me) return;
      devicesView(root, me);
    }).catch(function () {
      root.textContent = "";
      root.appendChild(el("div", "devwrap", "failed to load devices"));
    });
  }

  function devicesView(root, me) {
    root.textContent = "";
    var wrap = el("div", "devwrap");
    var head = el("div", "devhead");
    head.appendChild(el("h1", null, "eTerm"));
    head.appendChild(el("span", "meta", "tenant " + String(me.tenant || "").slice(0, 12)));
    head.appendChild(el("span", "spacer"));
    var logout = el("button", "btn", "logout");
    head.appendChild(logout);
    wrap.appendChild(head);
    var cards = el("div", "cards");
    wrap.appendChild(cards);
    root.appendChild(wrap);
    logout.addEventListener("click", function () {
      fetch("/api/v1/web/logout", { method: "POST" }).finally(function () {
        location.href = "/login";
      });
    });

    var hosts = me.hosts || [];
    var peers = me.peers || [];
    if (peers.length === 0) {
      cards.appendChild(el("div", "empty", "no devices online"));
    }
    peers.forEach(function (p) {
      cards.appendChild(deviceCard(root, wrap, p, hosts));
    });
  }

  function deviceCard(root, wrap, p, hosts) {
    var card = el("div", "card");
    var title = el("div", "title");
    title.appendChild(el("span", "nm", p.name || p.id));
    var online = p.online !== false;
    var badge = el("span", "badge" + (online ? "" : " off"), online ? "● online" : "○ offline");
    title.appendChild(badge);
    card.appendChild(title);
    var sub = el("div", "sub", p.id + (p.last_seen ? " · seen " + p.last_seen : ""));
    card.appendChild(sub);

    var actions = el("div", "actions");
    var btnShell = el("button", "btn", "New shell");
    var btnHosts = el("button", "btn", "Hosts");
    var btnTmux = el("button", "btn", "tmux");
    actions.appendChild(btnShell);
    actions.appendChild(btnHosts);
    actions.appendChild(btnTmux);
    card.appendChild(actions);

    var hostsPanel = el("div", "panel");
    var tmuxPanel = el("div", "panel");
    card.appendChild(hostsPanel);
    card.appendChild(tmuxPanel);

    btnShell.addEventListener("click", function () {
      openTerminal(root, wrap, {
        peerID: p.id, target: "local",
        title: (p.name || p.id) + " — shell"
      });
    });
    btnHosts.addEventListener("click", function () {
      tmuxPanel.classList.remove("open");
      btnTmux.classList.remove("on");
      if (hostsPanel.classList.toggle("open")) {
        btnHosts.classList.add("on");
        if (!hostsPanel.childNodes.length) {
          if (!hosts.length) {
            hostsPanel.appendChild(el("div", "empty", "no synced hosts"));
          }
          hosts.forEach(function (h) {
            var label = (h.alias || h.hostname) + "  " + (h.username ? h.username + "@" : "") + h.hostname + (h.port && h.port !== 22 ? ":" + h.port : "");
            var item = el("button", "item", label);
            item.addEventListener("click", function () {
              openTerminal(root, wrap, {
                peerID: p.id, target: "host", hostSyncID: h.sync_id,
                title: (p.name || p.id) + " — " + (h.alias || h.hostname)
              });
            });
            hostsPanel.appendChild(item);
          });
        }
      } else {
        btnHosts.classList.remove("on");
      }
    });
    btnTmux.addEventListener("click", function () {
      hostsPanel.classList.remove("open");
      btnHosts.classList.remove("on");
      if (!tmuxPanel.classList.toggle("open")) {
        btnTmux.classList.remove("on");
        return;
      }
      btnTmux.classList.add("on");
      tmuxPanel.textContent = "";
      tmuxPanel.appendChild(el("div", "empty", "loading..."));
      fetchTmuxList(p.id, function (err, list) {
        tmuxPanel.textContent = "";
        var addBtn = el("button", "item", "+ New tmux session");
        addBtn.addEventListener("click", function () {
          openTerminal(root, wrap, {
            peerID: p.id, target: "tmux-new",
            title: (p.name || p.id) + " — tmux"
          });
        });
        tmuxPanel.appendChild(addBtn);
        if (err) {
          tmuxPanel.appendChild(el("div", "empty", "tmux unavailable: " + err.message));
          return;
        }
        if (!list.length) {
          tmuxPanel.appendChild(el("div", "empty", "no tmux sessions"));
        }
        list.forEach(function (s) {
          var item = el("button", "item");
          item.appendChild(document.createTextNode(s.name + (s.attached ? "  (attached)" : "")));
          item.addEventListener("click", function () {
            openTerminal(root, wrap, {
              peerID: p.id, target: "tmux-attach", sessionID: s.session_id || s.name,
              title: (p.name || p.id) + " — tmux: " + s.name
            });
          });
          tmuxPanel.appendChild(item);
        });
      });
    });
    return card;
  }

  function openTerminal(root, wrap, opts) {
    wrap.style.display = "none";
    var session = new RelaySession({
      peerID: opts.peerID,
      target: opts.target,
      hostSyncID: opts.hostSyncID,
      sessionID: opts.sessionID
    });
    var holder = el("div");
    root.appendChild(holder);
    var screen = new TermScreen(holder, {
      title: opts.title,
      transport: session,
      onBack: function () {
        screen.destroy();
        holder.remove();
        wrap.style.display = "";
      },
      onfingerprint: function (info) {
        showFingerprintDialog(root, info, function () {
          session.confirmFingerprint(info);
        }, function () {
          session.close();
          screen.setStatus("exited: host key not trusted");
          screen.alive = false;
        });
      }
    });
  }

  // ---------- share view ----------

  function startShare(root, cfg) {
    var transport = new ShareTransport(cfg.token, new Date(cfg.expiresAt));
    new TermScreen(root, {
      title: cfg.name,
      expiresAt: new Date(cfg.expiresAt),
      transport: transport,
      onfingerprint: null
    });
  }

  // ---------- boot ----------

  var app = document.getElementById("app");
  var shareCfg = window.__ETERM_SHARE__;
  var m = location.pathname.match(/^\/x\/([^\/]+)\/?$/);
  if (m && shareCfg && shareCfg.token) {
    startShare(app, shareCfg);
    return;
  }
  if (location.pathname === "/login" || location.pathname.indexOf("/login/") === 0) {
    renderLogin(app);
    return;
  }
  renderDevices(app);
})();
