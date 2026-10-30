import { Rail } from "@/components/rail/Rail";
import { Footer } from "@/components/shell/Footer";
import { StockMain } from "@/components/shell/StockMain";
import { WalkthroughSlot } from "@/components/walkthrough/WalkthroughProvider";

export default function HomePage() {
  return (
    <div className="grid">
      <Rail />
      <main>
        <WalkthroughSlot name="main" className="walk-slot" />
        <StockMain />
        <Footer />
      </main>
    </div>
  );
}
