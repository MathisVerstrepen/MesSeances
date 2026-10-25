-- Minimal independent fixture: only columns the purge actually reads.
-- No Umami release metadata or unused primary-key columns are required.
CREATE TABLE public.session (session_id uuid, created_at timestamptz);
CREATE TABLE public.session_link (session_id uuid, created_at timestamptz);
CREATE TABLE public.website_event (event_id uuid, session_id uuid, created_at timestamptz);
CREATE TABLE public.event_data (website_event_id uuid, created_at timestamptz);
CREATE TABLE public.session_data (session_id uuid, created_at timestamptz);
CREATE TABLE public.revenue (session_id uuid, created_at timestamptz);
CREATE TABLE public.session_replay (session_id uuid, created_at timestamptz);
CREATE TABLE public.session_replay_saved (created_at timestamptz);
CREATE TABLE public.heatmap_event (session_id uuid, created_at timestamptz);
