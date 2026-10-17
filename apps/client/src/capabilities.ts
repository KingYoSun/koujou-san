export type ClientCapabilities = {
  surface: "web" | "native";
  localRunner: false;
  credentialAccessFromUI: false;
};

// Native detection describes the surface only; each assigned Run needs its own
// authorization and verified CLI capabilities. The UI grants no execution rights.
export function clientCapabilities(native: boolean): ClientCapabilities {
  return {
    surface: native ? "native" : "web",
    localRunner: false,
    credentialAccessFromUI: false,
  };
}
