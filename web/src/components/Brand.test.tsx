import { render, screen } from "@testing-library/react";
import { Brand } from "./Brand";

describe("Brand", () => {
  it("names the product in the main heading", () => {
    render(<Brand />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("PricedIn");
  });
});
