import { useLayoutEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { NativePortalContext } from "./components/assistant-ui/portal";
import { TooltipProvider } from "./components/ui/tooltip";
import styles from "./native-chat.css?inline";

// The application's global typography, buttons and --card token must not
// restyle the stock assistant-ui components (or vice versa).
export default function NativeChatSurface({ children }: { children: ReactNode }) {
  const host = useRef<HTMLDivElement>(null);
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    const element = host.current!;
    // Browsers register @property only at document scope. Keep Tailwind's
    // default utility values there; all selectors and resets stay isolated.
    if (!document.getElementById("native-chat-properties")) {
      const properties = document.createElement("style");
      properties.id = "native-chat-properties";
      properties.textContent = styles.match(/@property\s+--[\w-]+\s*\{[^}]*\}/g)?.join("\n") ?? "";
      document.head.append(properties);
    }
    const shadow = element.shadowRoot ?? element.attachShadow({ mode: "open" });
    const sheet = document.createElement("style"); sheet.textContent = styles;
    const root = document.createElement("div"); root.className = "h-full";
    shadow.replaceChildren(sheet, root);
    const syncTheme = () => {
      const dark = document.documentElement.dataset.theme !== "light";
      element.dataset.theme = dark ? "dark" : "light";
      root.classList.toggle("dark", dark);
    };
    syncTheme();
    const observer = new MutationObserver(syncTheme);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    setContainer(root);
    return () => observer.disconnect();
  }, []);
  return <div ref={host} className="assistant-chat-mount">{container && createPortal(<NativePortalContext.Provider value={container}><TooltipProvider>{children}</TooltipProvider></NativePortalContext.Provider>, container)}</div>;
}
