declare module "gopress" {
  export type NextFunction = (value?: "route" | string) => void

  export type Request = {
    params: Record<string, string>
    query: Record<string, string>
    body: Record<string, unknown>
    headers: Record<string, string>
    cookies: Record<string, string>
    method: string
    path: string
  }

  export type Response = {
    status(code: number): Response
    send(body: string): void
    json(value: Record<string, unknown>): void
    type(contentType: string): Response
    set(name: string, value: string): Response
    cookie(name: string, value: string): Response
    redirect(location: string): void
    redirect(status: number, location: string): void
    sendStatus(status: number): void
  }

  export type Handler = (req: Request, res: Response, next: NextFunction) => void | Promise<void>
  export type ErrorHandler = (err: Error, req: Request, res: Response, next: NextFunction) => void | Promise<void>

  export type RouteChain = {
    all(...handlers: Handler[]): RouteChain
    get(...handlers: Handler[]): RouteChain
    post(...handlers: Handler[]): RouteChain
    put(...handlers: Handler[]): RouteChain
    patch(...handlers: Handler[]): RouteChain
    delete(...handlers: Handler[]): RouteChain
    options(...handlers: Handler[]): RouteChain
    head(...handlers: Handler[]): RouteChain
  }

  export type RouterLike = {
    use(...handlers: Array<Handler | ErrorHandler>): RouterLike
    use(path: string, ...handlers: Array<Handler | ErrorHandler | RouterLike>): RouterLike
    all(path: string, ...handlers: Handler[]): RouterLike
    get(path: string, ...handlers: Handler[]): RouterLike
    post(path: string, ...handlers: Handler[]): RouterLike
    put(path: string, ...handlers: Handler[]): RouterLike
    patch(path: string, ...handlers: Handler[]): RouterLike
    delete(path: string, ...handlers: Handler[]): RouterLike
    options(path: string, ...handlers: Handler[]): RouterLike
    head(path: string, ...handlers: Handler[]): RouterLike
    route(path: string): RouteChain
  }

  export type App = RouterLike
  export type Router = RouterLike

  export function Router(): Router

  export type GopressFactory = {
    (): App
    json(): Handler
    static(root: string): Handler
  }

  const gopress: GopressFactory
  export default gopress
}
