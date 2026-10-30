import styles from "./Brand.module.css";

export const PRODUCT_PRINCIPLE =
  "PricedIn shows what must happen for an investment to succeed, not what the stock is supposedly worth.";

export function Brand() {
  return (
    <div className={styles.brand}>
      <h1>
        Priced<span>In</span>
      </h1>
    </div>
  );
}
