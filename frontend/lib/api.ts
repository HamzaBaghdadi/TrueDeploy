// Thin wrapper over the Wails-generated bindings, so the rest of the UI
// doesn't depend on generated paths. Regenerate with `wails3 generate bindings`.
import { Browser, Events } from "@wailsio/runtime";
import {
  ProjectService,
  ServerService,
  SetupService,
  type ContainerStatus,
  type DeployFinished,
  type DeployLog,
  type ProjectDetail,
  type ProjectInput,
  type ProjectView,
  type ServerInfo,
  type ServerInput,
  type SetupFinished,
  type SetupLog,
  type SetupOptions,
  type SetupPlan,
  type SetupStepEvent,
  type StepView,
} from "@/bindings/truedeploy/services";
import * as AppEvents from "@/bindings/events";
import type { Target as GoTarget } from "@/bindings/truedeploy/internal/sshx";
import type { Deployment, Server as GoServer } from "@/bindings/truedeploy/internal/store";

export type {
  ContainerStatus,
  Deployment,
  ProjectDetail,
  ProjectInput,
  ProjectView,
  ServerInfo,
  SetupFinished,
  SetupLog,
  SetupOptions,
  SetupPlan,
  SetupStepEvent,
  StepView,
};

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

  setupPlan: (serverId: string, opts: SetupOptions) => call(SetupService.Plan(serverId, opts)).then(required),
  runSetup: (serverId: string, stepIds: string[], opts: SetupOptions) =>
    call(SetupService.Run(serverId, stepIds, opts)),
  cancelSetup: (runId: string) => call(SetupService.Cancel(runId)),

  listProjects: async () => (await call(ProjectService.ListProjects())) ?? [],
  getProject: (id: string) => call(ProjectService.GetProject(id)).then(required),
  saveProject: (p: ProjectInput) => call(ProjectService.SaveProject(p)).then(required),
  deleteProject: (id: string) => call(ProjectService.DeleteProject(id)),
  deployKey: (serverId: string) => call(ProjectService.DeployKey(serverId)),
  checkRepoAccess: (projectId: string) => call(ProjectService.CheckRepoAccess(projectId)),
  deploy: (projectId: string) => call(ProjectService.Deploy(projectId)),
  cancelDeploy: (deploymentId: string) => call(ProjectService.CancelDeploy(deploymentId)),
  projectStatus: async (projectId: string) => (await call(ProjectService.Status(projectId))) ?? [],
  stopProject: (projectId: string) => call(ProjectService.StopProject(projectId)),

  // Opens in the system browser, not inside the app window.
  openURL: (url: string) => Browser.OpenURL(url),
};

export function onDeployEvents(handlers: {
  log: (e: DeployLog) => void;
  finished: (e: DeployFinished) => void;
}): () => void {
  const offs = [
    Events.On(AppEvents.DeployLog, (ev) => handlers.log(ev.data as DeployLog)),
    Events.On(AppEvents.DeployFinished, (ev) => handlers.finished(ev.data as DeployFinished)),
  ];
  return () => offs.forEach((off) => off());
}

// Subscribe to setup progress events. Returns an unsubscribe function.
export function onSetupEvents(handlers: {
  log: (e: SetupLog) => void;
  step: (e: SetupStepEvent) => void;
  finished: (e: SetupFinished) => void;
}): () => void {
  const offs = [
    Events.On(AppEvents.SetupLog, (ev) => handlers.log(ev.data as SetupLog)),
    Events.On(AppEvents.SetupStep, (ev) => handlers.step(ev.data as SetupStepEvent)),
    Events.On(AppEvents.SetupFinished, (ev) => handlers.finished(ev.data as SetupFinished)),
  ];
  return () => offs.forEach((off) => off());
}
