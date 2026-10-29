import { createContext, useContext } from "react";

// Keep popovers in the same style boundary as the official chat components.
export const NativePortalContext = createContext<HTMLElement | null>(null);
export const useNativePortal = () => useContext(NativePortalContext) ?? undefined;
