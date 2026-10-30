import { Suspense } from "react";
import { LoginForm } from "@/components/login/LoginForm";

export default function LoginPage() {
  return (
    <div className="wrap">
      <Suspense>
        <LoginForm />
      </Suspense>
    </div>
  );
}
