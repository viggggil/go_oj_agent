n=int(input()); a=[]
for _ in range(n):
    s,e=map(int,input().split()); a.append((e,s))
end=-1; ans=0
for e,s in sorted(a):
    if s>=end: ans+=1; end=e
print(ans)
