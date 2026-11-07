# UI conventions

The app uses **shadcn/ui** (Radix base, "nova" style) for every control and **lucide-react** for every icon.
No native `<button>`, `<input>`, `<select>`, `<textarea>` or checkbox styling in screens, and no text glyphs
used as icons (`←`, `→`, `✕`, `↗`, `✓`, `▶`, `•`).

## Palette (from the logo, `app/globals.css`, dark only)

Logo colours: cyan `#15AFEB` → azure `#1189EB` → royal `#105AE3` → navy `#102F8A`, silver `#DBE1E9`.
Surfaces are tinted with the logo's navy hue instead of neutral grey.

| Use for | Class | Value |
|---|---|---|
| Brand: primary actions, links, selection, focus | `primary`, `primary-foreground`, `ring` | `#1F6EF2` (royal, lifted for contrast), white |
| Healthy / done / live / connected | `success` | `#34D399` |
| In progress (deploying, connecting info) | `info` | `#22C3F0` (logo cyan) |
| Warnings | `warning` | `#F5B84A` |
| Errors, destructive | `destructive` (alias `danger`) | `#F87171` |
| Secondary text | `text-muted-foreground` | `#8B99B5` |
| Page / cards / popovers | `bg-background`, `bg-card`, `bg-popover` | `#070B14`, `#0C1322`, `#0E1628` |
| Layout layers, terminals | `bg-surface`, `bg-surface-2`, `bg-terminal` | |
| Hover / subtle fill | `bg-accent` / `bg-muted` (shadcn meanings) | |
| Lines | `border-border`, inputs `border-input` | |
| Logo colours | `brand-cyan`, `brand-azure`, `brand-royal`, `brand-navy`, `brand-silver` | |
| Logo gradient | `.bg-brand-gradient`, `.text-brand-gradient` | cyan → azure → royal |

Rule of thumb: **blue = "you can act here"**, **green = "this is healthy/done"**. Never use `primary` for success states.
## Components

| Need | Use |
|---|---|
| Primary action | `<Button>` |
| Secondary action | `<Button variant="outline">` |
| Low-emphasis / toolbar / "Cancel" | `<Button variant="ghost">` |
| Inline text action ("Check access", "Refresh") | `<Button variant="link" size="xs">` (or `ghost size="xs"`) |
| Destructive | `<Button variant="destructive">` |
| Sizes | `sm` for card headers & rows, `xs` for dense rows, `icon-*` for icon-only |
| Text field | `<Input>`; long text `<Textarea>`; label above with `<Label>` |
| Choice from list | `<Select>` + `SelectTrigger className="w-full"` + `SelectValue` + `SelectContent` + `SelectItem` |
| Checkbox | `<Checkbox checked onCheckedChange={(v) => ...(v === true)}>` with `<Label>` |
| On/off setting | `<Switch>` |
| 2–3 exclusive options (segmented) | `<ToggleGroup type="single" variant="outline">` |
| Option cards (radio) | `<RadioGroup>` + `RadioGroupItem` inside a clickable `<Label>` card |
| Status / tag | `<Badge variant="secondary|outline|destructive">` or the shared `DeployStatus` |
| Section box | `<Card>` + `CardHeader` (`CardTitle`, `CardDescription`, `CardAction`) + `CardContent` |
| Menu | `<DropdownMenu>` |
| Tabs | `<Tabs>` |
| Hover help on icon buttons | `<Tooltip>` (provider is in the layout) |
| Confirmations | `useConfirm()` from `components/confirm-dialog` (built on `AlertDialog`) |

## Shared helpers

- `BackButton` (`components/back-button`) for every "back to parent" link: `<BackButton label="Projects" onClick={...} />`
- `CopyButton` (`components/copy-button`): icon-only by default, or `label="Copy key"`
- `Notice` (`components/notice`): `<Notice tone="success|error|warning|info">` for result/error/warning messages
- `LogConsole` for command output, `DeployStatus` for deploy states

## Icons (lucide-react)

Inside `Button` icons size themselves; elsewhere use `className="size-4"` (or `size-3.5` in dense rows).

| Meaning | Icon |
|---|---|
| Back / forward / open detail | `ArrowLeft`, `ArrowRight`, `ChevronRight` |
| External link | `ExternalLink` |
| Close / remove | `X`; delete `Trash2` |
| Add | `Plus` |
| Done / success | `Check`, `CircleCheck`; failure `CircleX` / `CircleAlert`; warning `TriangleAlert` |
| Refresh / re-check | `RefreshCw` (add `animate-spin` while busy) |
| Busy | `Loader2 className="animate-spin"` |
| Deploy / run | `Rocket`, `Play`; stop `Square`; restart `RotateCw`; rollback `Undo2` |
| Logs / terminal | `ScrollText`, `SquareTerminal` |
| Show / hide secret | `Eye`, `EyeOff`; copy `Copy` |
| Settings / edit | `Settings`, `Pencil` |
| Server / project / database / git / key / domain | `Server`, `FolderGit2`, `Database`, `GitBranch`, `KeyRound`, `Globe` |
| GitHub | `GitHubMark` (components/github-mark) |

## Layout

Pages: `mx-auto max-w-5xl px-8 py-8`. Title `text-xl font-semibold tracking-tight`, subtitle
`text-sm text-muted-foreground`. Section labels `text-xs font-medium uppercase tracking-wide text-muted-foreground`.
Status dots stay plain `span`s (`size-2 rounded-full bg-primary`).
