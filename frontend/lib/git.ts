// What auto-deploy looks like on each Git provider, for the UI.

/** The web page of a repository from its clone URL (SSH or HTTPS). */
export function repoWeb(repoUrl: string): string | null {
  const u = repoUrl.trim();
  let host = "";
  let path = "";
  const scp = /^[A-Za-z0-9._-]+@([A-Za-z0-9.-]+):(.+)$/.exec(u);
  if (scp && !u.includes("://")) {
    [, host, path] = scp;
  } else {
    try {
      const p = new URL(u);
      host = p.hostname;
      path = p.pathname;
    } catch {
      return null;
    }
  }
  path = path.replace(/^\/+|\/+$/g, "").replace(/\.git$/, "");
  return host && path.includes("/") ? `https://${host}/${path}` : null;
}

export type CIInfo = {
  label: string; // GitHub
  ci: string; // GitHub Actions
  secrets: string; // where the key is saved
  commits: string[]; // what gets committed
  pipelinesUrl: string | null;
};

export function ciInfo(kind: string | undefined, slug: string, branch: string, repoUrl: string): CIInfo {
  const web = repoWeb(repoUrl);
  switch (kind) {
    case "gitlab":
      return {
        label: "GitLab",
        ci: "GitLab CI/CD",
        secrets: "CI/CD variables",
        commits: [`.gitlab/truedeploy-${slug}.yml`, `.gitlab-ci.yml including it (or the line to add, if you have your own)`],
        pipelinesUrl: web && `${web}/-/pipelines`,
      };
    case "bitbucket":
      return {
        label: "Bitbucket",
        ci: "Bitbucket Pipelines",
        secrets: "secured repository variables",
        commits: [`a step in bitbucket-pipelines.yml (or the step to add, if you have your own), with Pipelines turned on`],
        pipelinesUrl: web && `${web}/pipelines`,
      };
    case "gitea":
      return {
        label: "Gitea",
        ci: "Gitea Actions",
        secrets: "Actions secrets",
        commits: [`.gitea/workflows/truedeploy-${slug}.yml`],
        pipelinesUrl: web && `${web}/actions`,
      };
  }
  return {
    label: "GitHub",
    ci: "GitHub Actions",
    secrets: "encrypted GitHub secrets",
    commits: [`.github/workflows/truedeploy-${slug}.yml`],
    pipelinesUrl: web && `${web}/actions`,
  };
}
