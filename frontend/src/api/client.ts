import createClient from "openapi-fetch";
import type { paths } from "./schema";

// The typed client is generated from the backend's OpenAPI spec (ADR-0003).
// baseUrl "/" works in dev (Vite proxies /tasks to Go) and in production (Go
// serves both the frontend and the API on the same origin).
export const api = createClient<paths>({ baseUrl: "/" });

// Task shape pulled from the generated schema so the UI tracks the contract.
type ListBody =
  paths["/tasks"]["get"]["responses"]["200"]["content"]["application/json"];
type ActiveList = NonNullable<ListBody["active"]>;
export type Task = ActiveList[number];
