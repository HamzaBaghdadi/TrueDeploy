// Thin wrapper over the Wails-generated bindings, so the rest of the UI
// doesn't depend on generated paths. Regenerate with `wails3 generate bindings`.
import { Browser, Events } from "@wailsio/runtime";
import {
  AppService,
  DatabaseService,
  MonitorService,
  GitHubService,
  GitService,
  type GitAccountView,
  type GitConnectInput,
  ImportService,
  OffsiteService,
  type OffsiteCopy,
  type OffsiteView,
  ProjectService,
  ServerService,
  SetupService,
  TerminalService,
  type AppInput,
  type AdoptInput,
  type EnvGroupInput,
  type EnvGroupView,
  type AccessInput,
  type DetectInput,
  type PreviewInput,
  type JobView,
  type ProjectJobs,
  type SiteHistory,
  type Overview,
  type DetectResult,
  type ImportCompose,
  type ImportDatabase,
  type ImportDBInput,
  type ImportScan,
  type AutoDeployRuns,
  type ChannelInput,
  type ChannelView,
  type AppInfo,
  type Backup,
  type BackupScheduleStatus,
  type Connection,
  type ContainerStatus,
  type DatabaseInput,
  type DatabaseStatus,
  type DatabaseView,
  type DeployFinished,
  type DeployLog,
  type DeploymentEntry,
  type LogsEnd,
  type MonitorStatus,
  type LogsLine,
  type DomainCheck,
  type DomainsStatus,
  type GitHubRepo,
  type GitHubStatus,
  type NewGitHubRepo,
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
  type TermData,
  type TermExit,
  type UpdateStatus,
} from "@/bindings/truedeploy/services";
import * as AppEvents from "@/bindings/events";
import type { Target as GoTarget } from "@/bindings/truedeploy/internal/sshx";
import type { Container, DiskUsage, Metrics } from "@/bindings/truedeploy/internal/monitor";
import type { Engine, Schedule } from "@/bindings/truedeploy/internal/databases";
import type { DBLink, Deployment, Server as GoServer, Settings as AppSettings } from "@/bindings/truedeploy/internal/store";
import type { Alert } from "@/bindings/truedeploy/internal/alerts";
import type { Template, Var as TemplateVar } from "@/bindings/truedeploy/internal/templates";
import type { Job as JobInput } from "@/bindings/truedeploy/internal/jobs";
import type { Preset, Option as PresetOption } from "@/bindings/truedeploy/internal/presets";
import type { Target as OffsiteTarget } from "@/bindings/truedeploy/internal/offsite";
import type { NewRepo as NewGitRepo, Repo as GitRepo } from "@/bindings/truedeploy/internal/gitprov";

export type { GitAccountView, GitConnectInput, GitRepo, NewGitRepo };
export type GitKind = "github" | "gitlab" | "bitbucket" | "gitea";

export type { OffsiteCopy, OffsiteTarget, OffsiteView };
export type OffsiteKind = "database" | "project";

export type { Container, DiskUsage, Metrics, Engine, Schedule };
export type { Backup, BackupScheduleStatus, Connection, DatabaseStatus, DatabaseView, DBLink };
export type { Alert, AppInfo, AppSettings, ChannelInput, ChannelView, MonitorStatus, UpdateStatus };
export type { AppInput, Template, TemplateVar, Preset, PresetOption };
export type { AdoptInput, ImportCompose, ImportDatabase, ImportDBInput, ImportScan };
export type { DetectInput, DetectResult };
export type { JobView, ProjectJobs };
export type { SiteHistory, Overview };
export type { EnvGroupView };
export type { Job } from "@/bindings/truedeploy/internal/jobs";
export type { Suggestion } from "@/bindings/truedeploy/internal/detect";
export type {
  AutoDeployRuns,
  DeploymentEntry,
  ContainerStatus,
  Deployment,
  DomainCheck,
  DomainsStatus,
  GitHubRepo,
  GitHubStatus,
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
  serverMetrics: (serverId: string) => call(ServerService.Metrics(serverId)).then(required),
  serverContainers: async (serverId: string) => (await call(ServerService.Containers(serverId))) ?? [],
  containerAction: (serverId: string, name: string, action: "restart" | "stop" | "start") =>
    call(ServerService.ContainerAction(serverId, name, action)),
  containerLogs: async (serverId: string, name: string) => (await call(ServerService.ContainerLogs(serverId, name))) ?? [],
  dockerDiskUsage: async (serverId: string) => (await call(ServerService.DockerDiskUsage(serverId))) ?? [],
  pruneDocker: (serverId: string) => call(ServerService.PruneDocker(serverId)),

  setupPlan: (serverId: string, opts: SetupOptions) => call(SetupService.Plan(serverId, opts)).then(required),
  runSetup: (serverId: string, stepIds: string[], opts: SetupOptions) =>
    call(SetupService.Run(serverId, stepIds, opts)),
  cancelSetup: (runId: string) => call(SetupService.Cancel(runId)),

  listProjects: async () => (await call(ProjectService.ListProjects())) ?? [],
  getProject: (id: string) => call(ProjectService.GetProject(id)).then(required),
  saveProject: (p: ProjectInput) => call(ProjectService.SaveProject(p)).then(required),
  deleteProject: (id: string) => call(ProjectService.DeleteProject(id)),
  templates: async () => (await call(ProjectService.Templates())) ?? [],
  createApp: (a: AppInput) => call(ProjectService.CreateApp(a)).then(required),
  suggestPort: (serverId: string, templateId: string) => call(ProjectService.SuggestPort(serverId, templateId)),
  presets: async () => (await call(ProjectService.Presets())) ?? [],
  scanServer: (serverId: string) => call(ImportService.Scan(serverId)).then(required),
  adoptCompose: (a: AdoptInput) => call(ImportService.AdoptCompose(a)).then(required),
  importDatabase: (d: ImportDBInput) => call(ImportService.ImportDatabase(d)).then(required),
  detectFramework: (d: DetectInput) => call(ProjectService.DetectFramework(d)).then(required),
  listJobs: (projectId: string) => call(ProjectService.ListJobs(projectId)).then(required),
  saveJob: (projectId: string, job: JobInput) => call(ProjectService.SaveJob(projectId, job)),
  deleteJob: (projectId: string, jobId: string) => call(ProjectService.DeleteJob(projectId, jobId)),
  runJob: async (projectId: string, jobId: string) => (await call(ProjectService.RunJob(projectId, jobId))) ?? [],
  jobLog: async (projectId: string, jobId: string) => (await call(ProjectService.JobLog(projectId, jobId))) ?? [],
  branches: async (projectId: string) => (await call(ProjectService.Branches(projectId))) ?? [],
  previews: async (projectId: string) => (await call(ProjectService.Previews(projectId))) ?? [],
  createPreview: (p: PreviewInput) => call(ProjectService.CreatePreview(p)).then(required),
  deletePreview: (id: string) => call(ProjectService.DeletePreview(id)),
  setPreviewDomain: (projectId: string, domain: string) => call(ProjectService.SetPreviewDomain(projectId, domain)),
  setAccess: (a: AccessInput) => call(ProjectService.SetAccess(a)),
  envGroups: async () => (await call(ProjectService.ListEnvGroups())) ?? [],
  saveEnvGroup: (g: EnvGroupInput) => call(ProjectService.SaveEnvGroup(g)).then(required),
  deleteEnvGroup: (id: string) => call(ProjectService.DeleteEnvGroup(id)),
  redeployEnvGroup: (id: string) => call(ProjectService.RedeployEnvGroup(id)),
  previewPreset: async (id: string, opts: Record<string, string>, port: number) =>
    ((await call(ProjectService.PreviewPreset(id, opts, port))) ?? {}) as Record<string, string>,
  deployKey: (projectId: string) => call(ProjectService.DeployKey(projectId)),
  addDeployKey: (projectId: string) => call(ProjectService.AddDeployKey(projectId)),

  githubStatus: () => call(GitHubService.Status()).then(required),
  githubConnect: (token: string) => call(GitHubService.Connect(token)).then(required),
  githubDisconnect: () => call(GitHubService.Disconnect()),
  githubRepos: async () => (await call(GitHubService.Repos())) ?? [],
  githubOwners: async () => (await call(GitHubService.Owners())) ?? [],
  githubCreateRepo: (r: NewGitHubRepo) => call(GitHubService.CreateRepo(r)).then(required),
  gitAccounts: async () => (await call(GitService.Accounts())) ?? [],
  gitConnect: (i: GitConnectInput) => call(GitService.Connect(i)).then(required),
  gitDisconnect: (host: string) => call(GitService.Disconnect(host)),
  gitRepos: async (host: string) => (await call(GitService.Repos(host))) ?? [],
  gitOwners: async (host: string) => (await call(GitService.Owners(host))) ?? [],
  gitCreateRepo: (host: string, r: NewGitRepo) => call(GitService.CreateRepo(host, r)).then(required),
  checkRepoAccess: (projectId: string) => call(ProjectService.CheckRepoAccess(projectId)),
  deploy: (projectId: string) => call(ProjectService.Deploy(projectId)),
  rollback: (projectId: string, deploymentId: string) => call(ProjectService.Rollback(projectId, deploymentId)),
  streamLogs: (projectId: string, tail = 200, since = "") => call(ProjectService.StreamLogs(projectId, tail, since)),
  downloadLogs: (projectId: string, since: string) => call(ProjectService.DownloadLogs(projectId, since)),
  stopLogs: (streamId: string) => call(ProjectService.StopLogs(streamId)),
  recentDeployments: async (limit = 100) => (await call(ProjectService.RecentDeployments(limit))) ?? [],
  cancelDeploy: (deploymentId: string) => call(ProjectService.CancelDeploy(deploymentId)),
  projectStatus: async (projectId: string) => (await call(ProjectService.Status(projectId))) ?? [],
  stopProject: (projectId: string) => call(ProjectService.StopProject(projectId)),
  applyDomains: (projectId: string) => call(ProjectService.ApplyDomains(projectId)),
  checkDomains: (projectId: string) => call(ProjectService.CheckDomains(projectId)).then(required),
  enableAutoDeploy: (projectId: string) => call(ProjectService.EnableAutoDeploy(projectId)),
  disableAutoDeploy: (projectId: string) => call(ProjectService.DisableAutoDeploy(projectId)),
  autoDeployHistory: (projectId: string) => call(ProjectService.AutoDeployHistory(projectId)).then(required),

  dbEngines: async () => (await call(DatabaseService.Engines())) ?? [],
  listDatabases: async () => (await call(DatabaseService.ListDatabases())) ?? [],
  createDatabase: (d: DatabaseInput) => call(DatabaseService.CreateDatabase(d)).then(required),
  dbConnection: (id: string) => call(DatabaseService.Connection(id)).then(required),
  dbStatus: (id: string) => call(DatabaseService.Status(id)).then(required),
  dbRestart: (id: string) => call(DatabaseService.Restart(id)),
  deleteDatabase: (id: string, deleteData: boolean) => call(DatabaseService.DeleteDatabase(id, deleteData)),
  dbBackupNow: (id: string) => call(DatabaseService.BackupNow(id)),
  offsite: () => call(OffsiteService.Offsite()),
  saveOffsite: (t: OffsiteTarget) => call(OffsiteService.SaveOffsite(t)),
  removeOffsite: () => call(OffsiteService.RemoveOffsite()),
  offsiteCopies: (kind: OffsiteKind, id: string) => call(OffsiteService.OffsiteCopies(kind, id)).then((l) => l ?? []),
  fetchOffsite: (kind: OffsiteKind, id: string, name: string) => call(OffsiteService.FetchOffsite(kind, id, name)),
  downloadOffsite: (kind: OffsiteKind, id: string, name: string) => call(OffsiteService.DownloadOffsite(kind, id, name)),
  dbBackups: async (id: string) => (await call(DatabaseService.ListBackups(id))) ?? [],
  dbLogs: async (id: string) => (await call(DatabaseService.Logs(id))) ?? [],
  dbBackupSchedule: (id: string) => call(DatabaseService.BackupSchedule(id)).then(required),
  dbSetBackupSchedule: (id: string, s: Schedule) => call(DatabaseService.SetBackupSchedule(id, s)),
  dbRestore: (id: string, name: string) => call(DatabaseService.RestoreBackup(id, name)),
  dbDeleteBackup: (id: string, name: string) => call(DatabaseService.DeleteBackup(id, name)),
  /** Opens a Save dialog; resolves to the saved path, or "" if cancelled. */
  dbDownloadBackup: (id: string, name: string) => call(DatabaseService.DownloadBackup(id, name)),
  projectBackups: (id: string) => call(ProjectService.ProjectBackups(id)).then(required),
  backupProjectNow: (id: string) => call(ProjectService.BackupProjectNow(id)),
  setProjectBackupSchedule: (id: string, s: Schedule) => call(ProjectService.SetProjectBackupSchedule(id, s)),
  restoreProjectBackup: (id: string, name: string) => call(ProjectService.RestoreProjectBackup(id, name)),
  deleteProjectBackup: (id: string, name: string) => call(ProjectService.DeleteProjectBackup(id, name)),
  downloadProjectBackup: (id: string, name: string) => call(ProjectService.DownloadProjectBackup(id, name)),

  termOpen: (serverId: string, cols: number, rows: number) => call(TerminalService.Open(serverId, cols, rows)),
  termOpenContainer: (serverId: string, container: string, cols: number, rows: number) =>
    call(TerminalService.OpenContainer(serverId, container, cols, rows)),
  useNewKey: (serverId: string) => call(ServerService.UseNewKey(serverId)),
  disablePasswordLogin: async (serverId: string) => {
    const [message, adminCommands] = await call(ServerService.DisablePasswordLogin(serverId));
    return { message, adminCommands };
  },
  termWrite: (id: string, data: string) => call(TerminalService.Write(id, data)),
  termResize: (id: string, cols: number, rows: number) => call(TerminalService.Resize(id, cols, rows)),
  termClose: (id: string) => call(TerminalService.Close(id)),

  monitorStatus: () => call(MonitorService.Status()),
  checkNow: () => call(MonitorService.CheckNow()),
  dismissAlert: (key: string) => call(MonitorService.Dismiss(key)),
  getSettings: () => call(MonitorService.GetSettings()),
  saveSettings: (s: AppSettings) => call(MonitorService.SaveSettings(s)),
  listChannels: async () => (await call(MonitorService.ListChannels())) ?? [],
  saveChannel: (c: ChannelInput) => call(MonitorService.SaveChannel(c)).then(required),
  deleteChannel: (id: string) => call(MonitorService.DeleteChannel(id)),
  testChannel: (c: ChannelInput) => call(MonitorService.TestChannel(c)),

  appInfo: () => call(AppService.Info()),
  openDataFolder: () => call(AppService.OpenDataFolder()),
  updateStatus: () => call(AppService.UpdateStatus()),
  overview: () => call(MonitorService.Overview()).then(required),
  serverHistory: async (serverId: string, hours: number) => (await call(MonitorService.ServerHistory(serverId, hours))) ?? [],
  siteHistory: async (projectId: string, hours: number) => (await call(MonitorService.SiteHistory(projectId, hours))) ?? [],
  exportStatus: () => call(AppService.ExportStatus()),
  exportData: (passphrase: string) => call(AppService.ExportData(passphrase)),
  pickBackup: () => call(AppService.PickBackup()),
  previewImport: (path: string, passphrase: string) => call(AppService.PreviewImport(path, passphrase)).then(required),
  importData: (path: string, passphrase: string, replace: boolean) => call(AppService.ImportData(path, passphrase, replace)),
  checkForUpdate: () => call(AppService.CheckForUpdate()),

  // Opens in the system browser, not inside the app window.
  openURL: (url: string) => Browser.OpenURL(url),
};

export function onAlertsChanged(fn: (s: MonitorStatus) => void): () => void {
  return Events.On(AppEvents.AlertsChanged, (ev) => fn(ev.data as MonitorStatus));
}

export function onTerminalEvents(handlers: { data: (e: TermData) => void; exit: (e: TermExit) => void }): () => void {
  const offs = [
    Events.On(AppEvents.TermData, (ev) => handlers.data(ev.data as TermData)),
    Events.On(AppEvents.TermExit, (ev) => handlers.exit(ev.data as TermExit)),
  ];
  return () => offs.forEach((off) => off());
}

export function onLogsEvents(handlers: { line: (e: LogsLine) => void; end: (e: LogsEnd) => void }): () => void {
  const offs = [
    Events.On(AppEvents.LogsLine, (ev) => handlers.line(ev.data as LogsLine)),
    Events.On(AppEvents.LogsEnd, (ev) => handlers.end(ev.data as LogsEnd)),
  ];
  return () => offs.forEach((off) => off());
}

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

export function onUpdateStatus(fn: (s: UpdateStatus) => void): () => void {
  return Events.On(AppEvents.UpdateStatus, (ev) => fn(ev.data as UpdateStatus));
}
