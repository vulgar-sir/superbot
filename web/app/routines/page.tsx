"use client";

import { redirect } from "next/navigation";

// Group routines now live in each group's info panel; keep old links working.
export default function RoutinesPage() {
  redirect("/");
}
