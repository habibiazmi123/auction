"use client";

import { useState, FormEvent } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card } from "@/components/ui/card";

interface AuthFormProps {
  mode: "login" | "register";
}

export function AuthForm({ mode }: AuthFormProps) {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<"buyer" | "seller">("buyer");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const endpoint = mode === "login" ? "/api/auth/login" : "/api/auth/register";
    const body = mode === "login" ? { email, password } : { email, password, role };

    try {
      const res = await fetch(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });

      const data = await res.json();

      if (!res.ok) {
        setError(data.error || "Something went wrong");
        return;
      }

      if (mode === "register") {
        router.push("/login");
      } else {
        router.push("/auctions");
      }
    } catch {
      setError("Network error. Is the API running?");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Card className="nm-card w-full max-w-md p-8">
      <h1 className="text-2xl font-semibold text-center mb-6">
        {mode === "login" ? "Welcome Back" : "Create Account"}
      </h1>

      <form onSubmit={handleSubmit} className="space-y-5">
        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="nm-input w-full"
            placeholder="you@example.com"
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="nm-input w-full"
            placeholder="••••••••"
          />
        </div>

        {mode === "register" && (
          <div className="space-y-2">
            <Label>Role</Label>
            <div className="flex gap-3">
              {(["buyer", "seller"] as const).map((r) => (
                <button
                  key={r}
                  type="button"
                  onClick={() => setRole(r)}
                  className={`flex-1 py-2 rounded-xl text-sm font-medium transition-all
                    ${role === r ? "nm-inset text-[var(--color-primary)]" : "nm-btn"}`}
                >
                  {r.charAt(0).toUpperCase() + r.slice(1)}
                </button>
              ))}
            </div>
          </div>
        )}

        {error && (
          <div className="nm-inset p-3 border-l-4 border-[var(--color-accent)] text-sm text-[var(--color-accent)]">
            {error}
          </div>
        )}

        <Button type="submit" disabled={loading} className="nm-btn nm-btn-primary w-full text-base py-5">
          {loading ? "Please wait..." : mode === "login" ? "Sign In" : "Create Account"}
        </Button>
      </form>

      <p className="text-center text-sm text-[var(--color-text-muted)] mt-5">
        {mode === "login" ? (
          <>
            Don&apos;t have an account?{" "}
            <a href="/register" className="text-[var(--color-primary)] hover:underline">
              Register
            </a>
          </>
        ) : (
          <>
            Already have an account?{" "}
            <a href="/login" className="text-[var(--color-primary)] hover:underline">
              Sign In
            </a>
          </>
        )}
      </p>
    </Card>
  );
}
