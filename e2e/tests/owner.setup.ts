import { expect, test as setup } from "./support/fixtures";
import { baseURL, newClientIP, owner, ownerState } from "./support/stack";

// One login for the whole run: the tests share the owner's session, so they
// do not spend the login rate limit.
setup("the owner logs in", async ({ request }) => {
  const r = await request.post("/api/auth/login", {
    headers: { Origin: new URL(baseURL).origin, "x-real-ip": newClientIP() },
    data: owner,
  });
  expect(r.status(), await r.text()).toBe(200);
  await request.storageState({ path: ownerState });
});
