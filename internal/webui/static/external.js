// VS Code runs in a same-origin iframe. Wails' macOS webview does not create
// windows for window.open or target=_blank, and an ordinary browser blocks the
// popup because the sign-in URL only arrives after the user gesture has ended.
// Either way the external URL is handed to the host browser instead.
//
// VS Code does not open the sign-in URL directly. While the gesture is still
// active it reserves a blank popup with an argument-less window.open(), then
// assigns location.href once the auth extension produces the URL. A failed
// reservation aborts sign-in, so the reservation must always yield a window.
window.tsessionPopupBridge = {
  install(frame, { native, openExternal, onError }) {
    function external(url) {
      try {
        Promise.resolve(openExternal(url)).catch(onError);
      } catch (error) {
        onError(error);
      }
    }

    function reservedWindow() {
      const location = {
        get href() {
          return "about:blank";
        },
        set href(value) {
          external(String(value));
        },
        assign(value) {
          external(String(value));
        },
        replace(value) {
          external(String(value));
        },
        toString() {
          return "about:blank";
        },
      };
      return {
        closed: false,
        opener: null,
        location,
        document: {
          title: "",
          documentElement: { style: { cssText: "" } },
          body: { style: { cssText: "" }, textContent: "" },
        },
        focus() {},
        blur() {},
        close() {
          this.closed = true;
        },
      };
    }

    // noopener makes window.open return null even on success, which hides
    // whether a popup was blocked. Ask for the popup without it and re-apply
    // the intent afterwards so the outcome stays observable.
    function tryOpen(open, args, features) {
      const wantsNoOpener = /(^|,)\s*(noopener|noreferrer)\s*(,|$)/i.test(features || "");
      let popup = null;
      try {
        popup = open(...args);
      } catch (error) {
        return null;
      }
      if (popup && wantsNoOpener) {
        try {
          popup.opener = null;
        } catch (error) {
          /* a cross-origin popup refuses this; the request itself still stands */
        }
      }
      return popup;
    }

    frame.addEventListener("load", () => {
      try {
        const win = frame.contentWindow;
        const doc = frame.contentDocument;
        if (!win || !doc) throw new Error("VS Code frame is not same-origin");

        const originalOpen = win.open.bind(win);
        win.open = (url, target, features) => {
          const stripped = String(features || "")
            .split(",")
            .map((part) => part.trim())
            .filter((part) => part && !/^(noopener|noreferrer)$/i.test(part))
            .join(",");

          if (!url || String(url) === "about:blank") {
            if (native) return reservedWindow();
            return tryOpen(originalOpen, [], features) || reservedWindow();
          }

          const href = new URL(String(url), win.location.href).href;
          if (!/^https?:$/.test(new URL(href).protocol)) {
            return originalOpen(url, target, features);
          }

          if (native) {
            external(href);
            return reservedWindow();
          }
          const popup = tryOpen(originalOpen, [url, target, stripped || undefined], features);
          if (popup) return popup;
          external(href);
          return reservedWindow();
        };

        if (native) {
          doc.addEventListener("click", (event) => {
            const link = event.target.closest && event.target.closest('a[href][target="_blank"]');
            if (!link) return;
            event.preventDefault();
            external(link.href);
          }, true);
        }
      } catch (error) {
        onError(error);
      }
    });
  },
};
