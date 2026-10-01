declare module "jsdom" {
  export class JSDOM {
    constructor(html: string);
    readonly window: Record<string, any>;
  }
}

declare module "mermaid/dist/mermaid.core.mjs" {
  const mermaid: any;
  export default mermaid;
}
