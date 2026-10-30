import { Brand, PRODUCT_PRINCIPLE } from "@/components/Brand";
import styles from "./page.module.css";

export default function HomePage() {
  return (
    <main className="wrap">
      <section className={styles.hero}>
        <div className="card">
          <Brand />
          <p className="eyebrow">
            What must this company deliver for you to earn your target return?
          </p>
          <p className={styles.principle}>{PRODUCT_PRINCIPLE}</p>
          <p className="muted">Version 1 is in development.</p>
        </div>
      </section>
    </main>
  );
}
