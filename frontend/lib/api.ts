// Thin wrapper over the Wails-generated bindings, so the rest of the UI
// doesn't depend on generated paths. Regenerate with `wails3 generate bindings`.
import { ServerService, type ServerInfo, type ServerInput } from "@/bindings/truedeploy/services";
import type { Target as GoTarget } from "@/bindings/truedeploy/internal/sshx";
import type { Server as GoServer } from "@/bindings/truedeploy/internal/store";

export type { ServerInfo };

// Go's AuthMethod is generated as a TS enum; the UI uses the plain string values.
export type AuthMethod = "password" | "key";
export type Target = Omit<GoTarget, "auth"> & { auth: AuthMethod };
export type Server = Omit<GoServer, "auth"> & { auth: AuthMethod };

// Go errors arrive as rejected promises; normalise them to a plain message.
async function call<T>(p: Promise<T>): Promise<T> {
  try {
    return await p;
  } catch (e) {
    const msg = e instanceof Error ? e.message : typeof e === "string" ? e : JSON.stringify(e);
    throw new Error(msg);
  }
}

// Go returns (*T, error); on success the pointer is never nil.
const required = <T,>(v: T | null): T => v as T;

export const api = {
  testConnection: (t: Target) => call(ServerService.TestConnection(t as GoTarget)).then(required),
  saveServer: (name: string, t: Target) =>
    call(ServerService.SaveServer({ name, ...t } as ServerInput)).then(required),
  listServers: async () => ((await call(ServerService.ListServers())) ?? []) as Server[],
  deleteServer: (id: string) => call(ServerService.DeleteServer(id)),
  testSavedServer: (id: string) => call(ServerService.TestSavedServer(id)).then(required),
  pickKeyFile: () => call(ServerService.PickKeyFile()),
};
