import sys
it=iter(map(int,sys.stdin.buffer.read().split())); n=next(it); q=next(it); p=[0]
for _ in range(n): p.append(p[-1]+next(it))
for _ in range(q):
    l=next(it); r=next(it); print(p[r]-p[l-1])
