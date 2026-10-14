export type ClientCapabilities = {
  surface: "web" | "native";
  localRunner: false;
  credentialAccessFromUI: false;
};

// Native detection describes the surface only. It grants no execution rights
// and does not turn the unimplemented Runner into an available capability.
export function clientCapabilities(native: boolean): ClientCapabilities {
  return {
    surface: native ? "native" : "web",
    localRunner: false,
    credentialAccessFromUI: false,
  };
}
