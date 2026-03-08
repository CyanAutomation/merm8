# merm8-splash: Splash UI for merm8 Mermaid Linter

## Overview

**merm8-splash** is a separate, standalone **static site frontend** for the merm8 API. It provides an interactive, Bubble Tea-inspired terminal aesthetic interface where users can:

1. Point to any merm8 API endpoint (cloud-hosted or self-hosted)
2. Write/paste Mermaid diagram code
3. See real-time linting results with rule violations
4. Export analysis as SARIF, JSON, or markdown
5. Configure which rules to enforce

The project builds to pure static HTML/CSS/JS—zero backend required. Deploy to Vercel, Netlify, S3, or Docker.

---

## Technology Stack

### Framework & Build

- **Next.js 14+** with `output: 'export'` (static site generation)
- **TypeScript** (strict mode)
- **Tailwind CSS** with custom Bubble Tea theme
- **React 18+** for components

### Libraries

- **mermaid-js** — Live diagram rendering
- **Prism.js** — Syntax-highlighted code input
- **Axios** — HTTP client for merm8 API
- **clsx** — Conditional styling

### Deployment

- **Vercel** (primary: static site, auto-scaling, free tier)
- **Netlify** (alternative: static with functions support)
- **S3 + CloudFront** (DIY option)
- **Docker** (optional: self-hosted via container)

---

## Visual Design: Bubble Tea Aesthetic

### Color Palette

```css
/* Charmbracelet Bubble Tea inspired */
--color-bg-primary: #1e1e1e; /* Dark background */
--color-bg-secondary: #2a2a2a; /* Slightly lighter bg */
--color-border: #444444; /* Subtle borders */
--color-text-primary: #a0a0a0; /* Default text (neutral gray) */
--color-text-secondary: #707070; /* Muted text */
--color-accent-primary: #7571f9; /* Purple accent */
--color-accent-secondary: #a1efe4; /* Cyan accent */
--color-success: #04b575; /* Green (rule passed) */
--color-warning: #ffc107; /* Yellow (warning severity) */
--color-error: #ff5555; /* Red (error severity) */
--color-info: #7aa2f7; /* Blue (info severity) */

/* Font stack */
--font-mono: "IBM Plex Mono", "Courier New", monospace;
--font-size-base: 14px;
--line-height-base: 1.4;
```

### Design System

- **Borders**: 2px solid borders using primary accent color
- **Spacing**: 4px, 8px, 16px, 24px rhythm (Tailwind defaults)
- **Radius**: Minimal (0-2px) for harsh terminal look
- **Shadows**: Subtle (0 1px 3px rgba(0,0,0,0.3) on cards)
- **Typography**: Monospace everywhere; weights: 400 (regular), 600 (emphasis)

### ASCII-Style Elements

Where possible, use:

- Box-drawing characters (U+2500–U+257F) for dividers
- ▔▔▔ for visual separators (via CSS `border-top` or Unicode)
- ASCII spinners for loading: `⠋`, `⠙`, `⠹`, `⠸`, `⠼`, `⠴`, `⠦`, `⠧`, `⠇`, `⠏`

---

## Project Structure

```
merm8-splash/
├── app/
│   ├── layout.tsx              # Root layout, theme provider
│   ├── page.tsx                # Main page (dashboard)
│   ├── globals.css             # Tailwind + global styles
│   └── components/
│       ├── DiagramEditor.tsx        # Code editor with syntax highlighting
│       ├── DiagramPreview.tsx       # Live Mermaid render
│       ├── ApiConfigPanel.tsx       # API endpoint config UI
│       ├── RulesPanel.tsx           # Rule toggles + descriptions
│       ├── ResultsPanel.tsx         # Violations table/list
│       ├── StatusBar.tsx            # Bottom status bar
│       ├── ExportDropdown.tsx       # Export options
│       ├── ErrorBoundary.tsx        # Error handling wrapper
│       └── LoadingSpinner.tsx       # ASCII-style loader
├── lib/
│   ├── api.ts                  # API client & endpoint resolution
│   ├── useApiEndpoint.ts       # Hook for API config
│   ├── useDiagramAnalysis.ts   # Hook for analysis state
│   ├── keyboard.ts             # Keyboard shortcut handlers
│   └── theme.ts                # Tailwind color constants
├── public/
│   └── favicon.ico
├── next.config.js              # Static export config
├── tailwind.config.ts          # Bubble Tea theme
├── tsconfig.json               # TypeScript config
├── package.json                # Dependencies & scripts
├── .env.example                # Example environment variables
├── vercel.json                 # Vercel deployment config
├── netlify.toml                # Netlify deployment config
├── Dockerfile                  # Optional: Docker image
├── README.md                   # Project setup & deployment
└── merm8-splash-design.md      # This file (specification)
```

---

## Implementation Plan

### Phase 1: Project Setup & Theming ✓

1. Initialize Next.js 14 with TypeScript
2. Configure Tailwind CSS with custom Bubble Tea theme
3. Set up global styles (globals.css) with:
   - CSS reset and base styles
   - Color helper classes
   - Severity color mappings
   - Button and indicator utilities
4. Create app/layout.tsx with root structure
5. Create app/page.tsx stub for main page
6. **Deliverables**:
   - next.config.js with `output: 'export'`
   - tailwind.config.ts with Bubble Tea colors
   - app/globals.css with utility classes
   - app/layout.tsx with theme provider
   - app/page.tsx with basic structure
   - package.json with all dependencies

### Phase 2: Component Architecture

1. Create `lib/theme.ts` with color constants exported
2. Build reusable components in `app/components/`:
   - `DiagramEditor.tsx` — Textarea or Prism-highlighted code input
   - `DiagramPreview.tsx` — mermaid.render() container with error handling
   - `ApiConfigPanel.tsx` — Input field, connection status, test button
   - `RulesPanel.tsx` — Collapsible rule list with toggle switches
   - `ResultsPanel.tsx` — Violations table (rule, severity, message, line)
   - `StatusBar.tsx` — Read-only status indicators
   - `ExportDropdown.tsx` — Menu with SARIF/JSON/Markdown options
   - `ErrorBoundary.tsx` — Error boundary wrapper
   - `LoadingSpinner.tsx` — ASCII spinner animation
3. **Verification**: Each component renders in isolation with TypeScript strict mode

### Phase 3: API Integration

1. Create `lib/api.ts`:
   - `resolveApiEndpoint()` — Read from URL param, localStorage, env var, default
   - `createApiClient(endpoint: string)` — Axios instance with error handling
   - `fetchHealthz(endpoint)` — `GET /v1/healthz` to test connection
   - `fetchRules(endpoint)` — `GET /v1/rules` to get rule metadata
   - `analyzeCode(endpoint, code, enabledRules)` — `POST /v1/analyze` with config
2. Create `lib/useApiEndpoint.ts`:
   - Hook to manage API endpoint state
   - Save/load from localStorage
   - Validate URL format
3. Create `lib/useDiagramAnalysis.ts`:
   - Hook to manage diagram code, analysis state, error state
   - Debounce analysis requests (500ms)
   - Cache results to avoid redundant calls
4. **Verification**: All API calls work against live merm8 API, errors display gracefully

### Phase 4: Main Page Layout & Integration

1. Implement `app/page.tsx` with responsive grid:
   - Desktop: 2-column (editor left 40%, preview/results right 60%)
   - Mobile: Stacked full-width
2. Wire components together:
   - ApiConfigPanel → resolveApiEndpoint(), test connection
   - DiagramEditor → useDiagramAnalysis (code input)
   - DiagramPreview → render code, show parse errors
   - RulesPanel → fetchRules(), toggle enabled rules (localStorage)
   - ResultsPanel → populate from analysis results
   - StatusBar → show connection, parse status, rule count
   - ExportDropdown → export to SARIF/JSON/Markdown
3. **Verification**: Full end-to-end flow works, edit diagram → see results

### Phase 5: Keyboard Shortcuts & Polish

1. Create `lib/keyboard.ts`:
   - `useKeyboardShortcuts()` hook
   - Ctrl+K → focus API input
   - Ctrl+E → focus editor
   - Tab → cycle focus
2. Enhance components:
   - Add keyboard hints to UI (e.g., "Press ? for help")
   - Inline syntax highlighting in editor
   - Rule tooltips from API data
   - Copy buttons on violations
3. **Verification**: Keyboard navigation works, animations smooth

### Phase 6: Responsive Design & Mobile UX

1. Test layouts on:
   - Desktop (1920px, 1440px, 1024px)
   - Tablet (768px)
   - Mobile (375px, 414px)
2. Adjust grid/flex layouts for breakpoints
3. Ensure touch-friendly button sizes (44px minimum)
4. **Verification**: Responsive on all viewports, no layout shifts

### Phase 7: Deployment Configuration

1. Create `vercel.json` for Vercel deployment
2. Create `netlify.toml` for Netlify deployment
3. Create `Dockerfile` for optional self-hosting
4. Create `.env.example` with all env vars
5. Create `README.md` with setup/deployment instructions
6. **Verification**: Build succeeds, outputs to `out/` folder

### Phase 8: Testing & Optimization

1. Manual testing checklist:
   - [ ] All API endpoints work
   - [ ] Real-time analysis functions
   - [ ] Export formats work
   - [ ] Responsive layouts correct
   - [ ] Error handling graceful
   - [ ] Keyboard shortcuts functional
2. Performance optimization:
   - Mermaid lazy-loading if needed
   - Code-split components for smaller initial bundle
   - Image optimization (if any)
3. Lighthouse scoring: > 90 across all metrics
4. **Verification**: All manual tests pass, no errors

---

## Key Components Detailed

### DiagramEditor

- **Purpose**: Accept Mermaid code input with syntax highlighting
- **Features**:
  - Multi-line textarea with monospace font
  - Line numbers (optional via library)
  - Prism.js syntax highlighting for Mermaid
  - Paste example button
  - Clear button
  - Focus indicator (cyan border)
- **State**: Code string, synced to localStorage
- **Output**: onChange event with updated code
- **Props**: `value: string`, `onChange: (code: string) => void`

### DiagramPreview

- **Purpose**: Live render diagram using mermaid-js
- **Features**:
  - Real-time updates on code change
  - Error display if diagram invalid
  - Responsive SVG sizing
  - Dark theme applied to rendered diagrams
- **State**: Diagram render state (loading, success, error)
- **Props**: `code: string`, `onError: (error) => void`
- **Fallback**: Error message display

### ApiConfigPanel

- **Purpose**: Display and configure merm8 API endpoint
- **Features**:
  - Text input for custom API URL
  - Connection status indicator (● green/gray/red)
  - "Test Connection" button (calls `/v1/healthz`)
  - Preset dropdown (Official API, localhost:8080, etc.)
  - Save to localStorage button
  - Display resolved config source
- **State**: API endpoint URL
- **Props**: `endpoint: string`, `onEndpointChange: (url: string) => void`, `connectionStatus: 'connected' | 'checking' | 'error'`

### RulesPanel

- **Purpose**: Show available rules and toggle enforcement
- **Features**:
  - Fetches rules from `GET /v1/rules` on mount
  - Rule list with name, description, severity
  - Toggle switches for enable/disable per rule
  - Collapsible rule details
  - Rule count badge
- **State**: Enabled rules array, rules metadata
- **Props**: `rules: Rule[]`, `enabledRules: string[]`, `onRulesChange: (rules: string[]) => void`

### ResultsPanel

- **Purpose**: Display analysis violations in table format
- **Features**:
  - Table: Rule ID | Severity Badge | Message | Line
  - Color-coded severity (red=error, yellow=warning, blue=info)
  - Clickable rows to jump to line in editor (emit event)
  - Filter by severity dropdown
  - Sort by severity/line number
  - Copy button per violation
  - Empty state: "No violations found ✓"
- **State**: Analysis results from API
- **Props**: `results: AnalysisResult[]`, `onJumpToLine: (line: number) => void`

### StatusBar

- **Purpose**: Display overall analysis status
- **Features**:
  - Connection status: "● API Connected" or "● API Unreachable"
  - Parse status: "Parsing...", "Valid", "Syntax error"
  - Rule stats: "3 rules enabled, 1 violation found"
  - Right side: API endpoint (truncated)
- **State**: Read-only (derived from other state)
- **Props**: `connectionStatus`, `parseStatus`, `ruleCount`, `violationCount`, `apiEndpoint`

### ExportDropdown

- **Purpose**: Export analysis results
- **Formats**:
  - SARIF JSON (download)
  - JSON (download)
  - Markdown (copy to clipboard)
  - Text (copy to clipboard)
- **State**: Recent export format
- **Props**: `results: AnalysisResult[]`, `code: string`

---

## API Integration

### Endpoint Discovery (Priority Order)

1. **URL Parameter**: `?api=https://custom-api.com` (highest priority)
2. **localStorage**: Key `merm8_api_endpoint` if set by user
3. **Environment Variable**: `NEXT_PUBLIC_MERM8_API_URL` (build-time)
4. **Default Fallback**: `https://api.merm8.app` (or prompt user)

**Implementation** (`lib/api.ts`):

```typescript
export function resolveApiEndpoint(): string {
  // 1. Check URL param
  if (typeof window !== "undefined") {
    const params = new URLSearchParams(window.location.search);
    if (params.has("api")) return params.get("api")!;

    // 2. Check localStorage
    const stored = localStorage.getItem("merm8_api_endpoint");
    if (stored) return stored;
  }

  // 3. Check env var
  if (process.env.NEXT_PUBLIC_MERM8_API_URL) {
    return process.env.NEXT_PUBLIC_MERM8_API_URL;
  }

  // 4. Fallback
  return "https://api.merm8.app";
}
```

### Key API Calls

#### 1. `GET /v1/healthz`

- **Purpose**: Verify API is reachable
- **Used by**: ApiConfigPanel (connection test)
- **Error Handling**: Red indicator if fails

#### 2. `GET /v1/rules`

- **Purpose**: Fetch available rules and metadata
- **Used by**: RulesPanel (on mount)
- **Cache**: In React state
- **Response**: `{ rules: [ { id, name, description, severity }, ... ] }`

#### 3. `POST /v1/analyze`

- **Purpose**: Analyze Mermaid code
- **Called**: On editor change (debounced 500ms)
- **Request**:
  ```json
  {
    "code": "graph TD\nA-->B",
    "config": {
      "schema-version": "v1",
      "rules": {
        "no-cycles": { "enabled": true },
        "max-fanout": { "enabled": true, "limit": 5 }
      }
    }
  }
  ```
- **Response** (success):
  ```json
  {
    "diagram_type": "flowchart",
    "results": [
      {
        "rule_id": "max-fanout",
        "severity": "warning",
        "message": "Node exceeds limit: 6 > 5",
        "node_id": "A",
        "line": 1
      }
    ]
  }
  ```
- **Error Handling**: Display user-friendly messages

#### 4. `POST /v1/analyze/sarif` (Optional)

- **Purpose**: Get SARIF 2.1.0 format for export
- **Used by**: ExportDropdown (SARIF export button)

---

## Layout & Responsive Design

### Desktop (>1024px)

```
┌────────────────────────────────────────────────────────┐
│              API Config Panel (top)                     │
├──────────────────────┬──────────────────────────────────┤
│                      │                                   │
│  Diagram Editor      │   Diagram Preview + Results       │
│  (left, 40%)         │   (right, 60%)                    │
│                      │                                   │
├──────────────────────┼──────────────────────────────────┤
│     Rules Panel      │      Export / Status Bar          │
│     (bottom-left)    │      (bottom-right)               │
└──────────────────────┴──────────────────────────────────┘
```

### Tablet (768–1024px)

- Editor: 50%, Preview: 50% (stacked side-by-side)
- Rules panel moves below

### Mobile (<768px)

- Full-width stacked layout

---

## Keyboard Shortcuts

| Shortcut           | Action                    |
| ------------------ | ------------------------- |
| `Ctrl+K` / `Cmd+K` | Focus API config input    |
| `Ctrl+E` / `Cmd+E` | Focus editor              |
| `Ctrl+R` / `Cmd+R` | Focus results panel       |
| `Tab`              | Cycle focus to next panel |
| `Shift+Tab`        | Cycle to previous panel   |

---

## Build & Deployment

### next.config.js

```javascript
/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "export",
  basePath: process.env.NEXT_PUBLIC_BASE_PATH || "",
  assetPrefix: process.env.NEXT_PUBLIC_ASSET_PREFIX || "",
};
module.exports = nextConfig;
```

### Environment Variables (.env.example)

```bash
NEXT_PUBLIC_MERM8_API_URL=https://api.merm8.app
NEXT_PUBLIC_BASE_PATH=
NEXT_PUBLIC_ASSET_PREFIX=
```

### vercel.json

```json
{
  "buildCommand": "next build",
  "outputDirectory": "out",
  "env": {
    "NEXT_PUBLIC_MERM8_API_URL": "@merm8_api_url"
  }
}
```

### Deployment Checklist

- [ ] Build succeeds: `npm run build` produces `out/` folder
- [ ] No errors in build output
- [ ] Static files generated (HTML, CSS, JS bundles)
- [ ] `out/index.html` exists and is valid
- [ ] All assets linked correctly (no 404s)
- [ ] Bundle size < 200 KB (excluding node_modules)
- [ ] Lighthouse score > 90
- [ ] Vercel deployment succeeds
- [ ] Test against live merm8 API
- [ ] Mobile responsive verified

---

## Performance Targets

- **Bundle Size**: < 200 KB (gzipped)
- **Lighthouse Score**: > 90 (Performance, Accessibility, Best Practices)
- **First Contentful Paint**: < 2s
- **Analysis debounce**: 500ms
- **Mermaid render**: < 300ms for typical diagrams

---

## Testing Checklist

### Manual Testing

- [ ] API endpoint config: URL param, localStorage, env var work
- [ ] Real-time analysis: edit diagram → see results within 500ms
- [ ] Results rendering: correct severities, colors
- [ ] Rules panel: fetches rules, toggles work, localStorage persists
- [ ] Export: SARIF, JSON, Markdown formats work
- [ ] Responsive: desktop (1920px), tablet (768px), mobile (375px)
- [ ] Error handling: invalid API URL, parse timeout, network error
- [ ] Keyboard: Ctrl+K, Ctrl+E, Tab work
- [ ] Persistence: reload page, state retained

### Browser Testing

- [ ] Chrome/Edge (latest)
- [ ] Firefox (latest)
- [ ] Safari (latest)
- [ ] Mobile Safari (iOS)
- [ ] Chrome Mobile (Android)

---

## Future Enhancements (Out of Scope)

1. **Diagram Examples Library** — Preloaded sample diagrams
2. **Rule Explanations** — Detailed docs modal
3. **Batch Analysis** — Multiple file uploads
4. **GitHub Integration** — OAuth, PR analysis
5. **Vim Keybinds** — Full vim-mode editor
6. **Dark/Light Theme** — Alternative color schemes
7. **Mermaid Version Selector** — Per-diagram version choice
8. **Analytics** — Track popular rules

---

## References

- [merm8 API Guide](../API_GUIDE.md)
- [Next.js Static Export Docs](https://nextjs.org/docs/app/building-your-application/deploying/static-exports)
- [Tailwind CSS Documentation](https://tailwindcss.com/docs)
- [Mermaid.js Documentation](https://mermaid.js.org/)
- [SARIF Specification](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
